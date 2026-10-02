package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
)

// A portrait-sized transparent PNG with an opaque block in the middle.
func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := h / 4; y < h*3/4; y++ {
		for x := w / 4; x < w*3/4; x++ {
			img.Set(x, y, color.NRGBA{200, 60, 40, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func (h *harness) addItem(fields map[string]string, file []byte) (int, map[string]any) {
	h.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("file", "portrait.png")
	_, _ = fw.Write(file)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", h.url("/api/library/items"), &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatalf("add item: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (h *harness) items(query string) (int, []map[string]any) {
	h.t.Helper()
	code, body := h.do("GET", "/api/library/items"+query, nil)
	var out struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	_ = json.Unmarshal(body, &out)
	if code != http.StatusOK {
		h.t.Fatalf("list items%s: HTTP %d: %s", query, code, body)
	}
	return out.Total, out.Items
}

// The library starts with shelves to put things on, and a picture added to one
// comes back with its size, a thumbnail and a searchable description.
func TestLibraryAddSearchAndThumbnail(t *testing.T) {
	h := start(t)

	code, body := h.do("GET", "/api/library/categories", nil)
	var cats struct {
		Categories []LibraryCategory `json:"categories"`
	}
	_ = json.Unmarshal(body, &cats)
	if code != http.StatusOK || len(cats.Categories) != len(defaultCategories) || cats.Categories[0].Name != "人像" {
		t.Fatalf("default categories = %+v (HTTP %d)", cats.Categories, code)
	}

	pic := testPNG(t, 600, 900)
	code, out := h.addItem(map[string]string{
		"category": "人像", "name": "指着屏幕", "tags": "指向,惊讶", "description": "单手指向右上方，适合做推荐类封面",
	}, pic)
	if code != http.StatusCreated {
		t.Fatalf("add: HTTP %d: %v", code, out)
	}
	item := out["item"].(map[string]any)
	if item["width"].(float64) != 600 || item["height"].(float64) != 900 {
		t.Fatalf("size = %v×%v, want 600×900", item["width"], item["height"])
	}

	// Terms separated by spaces must all match, across tags and description.
	if n, _ := h.items("?q=" + "指向%20推荐"); n != 1 {
		t.Errorf("search 指向 推荐 = %d, want 1", n)
	}
	if n, _ := h.items("?q=" + "指向%20开心"); n != 0 {
		t.Errorf("search 指向 开心 = %d, want 0", n)
	}
	if n, _ := h.items("?category=%E8%83%8C%E6%99%AF"); n != 0 { // 背景
		t.Errorf("背景 should be empty, got %d", n)
	}
	if code, _ := h.do("GET", "/api/library/items?category=nope", nil); code != http.StatusNotFound {
		t.Errorf("unknown category: HTTP %d, want 404", code)
	}

	// The thumbnail is a smaller PNG that keeps the transparency.
	resp, err := h.client.Get(h.url(item["thumb"].(string) + "?w=120"))
	if err != nil {
		t.Fatalf("thumb: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("thumb is not a PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 80 || b.Dy() != 120 {
		t.Errorf("thumb size = %v, want 80×120", b)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Errorf("thumb corner alpha = %d, want transparent", a)
	}
}

// Re-running an import must update the items it brought in before, not add a
// second copy of every portrait.
func TestLibraryReimportUpdatesInPlace(t *testing.T) {
	h := start(t)
	pic := testPNG(t, 40, 60)
	fields := map[string]string{"category": "人像", "name": "P001", "source": "feishu:rec1"}
	if code, out := h.addItem(fields, pic); code != http.StatusCreated {
		t.Fatalf("first add: HTTP %d: %v", code, out)
	}
	fields["name"] = "P001 改名"
	code, out := h.addItem(fields, pic)
	if code != http.StatusOK || out["created"] != false {
		t.Fatalf("second add: HTTP %d created=%v, want 200 and an update", code, out["created"])
	}
	n, items := h.items("")
	if n != 1 || items[0]["name"] != "P001 改名" {
		t.Fatalf("items = %d %v, want the one item, renamed", n, items)
	}
}

// A category that still holds pictures cannot be deleted by accident.
func TestLibraryRefusesToDeleteANonEmptyCategory(t *testing.T) {
	h := start(t)
	code, out := h.addItem(map[string]string{"category": "APP Logo"}, testPNG(t, 20, 20))
	if code != http.StatusCreated {
		t.Fatalf("add: HTTP %d: %v", code, out)
	}
	if code, _ := h.do("DELETE", "/api/library/categories/APP%20Logo", nil); code != http.StatusConflict {
		t.Fatalf("delete non-empty: HTTP %d, want 409", code)
	}
	id := out["item"].(map[string]any)["id"].(string)
	if code, _ := h.do("PATCH", "/api/library/items/"+id, map[string]any{"category": "装饰元素", "tags": []string{"图标"}}); code != http.StatusOK {
		t.Fatalf("move item: HTTP %d", code)
	}
	if code, _ := h.do("DELETE", "/api/library/categories/APP%20Logo", nil); code != http.StatusOK {
		t.Fatalf("delete emptied category: HTTP %d, want 200", code)
	}
}

// A cut-out on a big transparent frame is cropped to what is visible when
// asked to, so it fits a cover by the person, not by the empty canvas.
func TestLibraryTrimsTransparentMargins(t *testing.T) {
	h := start(t)
	code, out := h.addItem(map[string]string{"category": "人像", "trim": "true"}, testPNG(t, 400, 200))
	if code != http.StatusCreated {
		t.Fatalf("add: HTTP %d: %v", code, out)
	}
	it := out["item"].(map[string]any)
	// The opaque block is the middle half (200×100) plus a 4px margin each side.
	if it["width"].(float64) != 208 || it["height"].(float64) != 108 {
		t.Fatalf("trimmed size = %v×%v, want 208×108", it["width"], it["height"])
	}
}

// A term in the tags ranks above the same words buried in a description.
func TestLibrarySearchRanksTagsFirst(t *testing.T) {
	h := start(t)
	pic := testPNG(t, 20, 20)
	h.addItem(map[string]string{"category": "人像", "name": "托举", "description": "看向左掌，不要把标题放在指向左上的位置"}, pic)
	h.addItem(map[string]string{"category": "人像", "name": "指着", "tags": "指向左上"}, testPNG(t, 22, 22))
	h.addItem(map[string]string{"category": "人像", "name": "另一张", "description": "随便说说"}, testPNG(t, 24, 24))
	n, items := h.items("?q=%E6%8C%87%E5%90%91%E5%B7%A6%E4%B8%8A") // 指向左上
	if n != 2 || items[0]["name"] != "指着" {
		t.Fatalf("results = %d, first %v; want the tagged one first", n, items[0]["name"])
	}
}
