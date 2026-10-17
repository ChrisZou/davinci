package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"davinci/internal/doc"
)

func paletteImage(t *testing.T, colors []color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			i := 0
			if x >= 60 && len(colors) > 1 {
				i = 1 + (x-60)*(len(colors)-1)/40
			}
			img.SetNRGBA(x, y, colors[i])
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func rgbHex(s string) paletteRGB {
	var r, g, b int
	fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b)
	return paletteRGB{float64(r), float64(g), float64(b)}
}
func TestExtractPaletteRoles(t *testing.T) {
	raw := paletteImage(t, []color.NRGBA{{240, 236, 220, 255}, {24, 30, 42, 255}, {236, 52, 24, 255}, {94, 90, 82, 255}, {20, 120, 180, 255}})
	p, err := ExtractPalette(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.BG != "#f0ecdc" || p.Text != "#181e2a" || p.Highlight != "#ec3418" || p.Accent != "#1478b4" {
		t.Fatalf("roles: %+v", p)
	}
	if paletteContrast(rgbHex(p.BG), rgbHex(p.Text)) < 7 || paletteContrast(rgbHex(p.BG), rgbHex(p.Text2)) < 4.5 || paletteContrast(rgbHex(p.BG), rgbHex(p.Text2)) >= paletteContrast(rgbHex(p.BG), rgbHex(p.Text)) {
		t.Fatal("contrast", p)
	}
	for i := 0; i < 10; i++ {
		again, _ := ExtractPalette(raw)
		if !reflect.DeepEqual(p, again) {
			t.Fatal("non-deterministic palette")
		}
	}
	for _, c := range []color.NRGBA{{0, 0, 0, 255}, {255, 255, 255, 255}, {128, 128, 128, 255}, {22, 90, 140, 128}} {
		p, err := ExtractPalette(paletteImage(t, []color.NRGBA{c}))
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Normalize(); err != nil {
			t.Fatal(err)
		}
		if p.Highlight == p.Accent || p.Text == p.BG || paletteContrast(rgbHex(p.BG), rgbHex(p.Text2)) < 4.45 {
			t.Fatal("monochrome derivation", p)
		}
	}
	if _, err := ExtractPalette(paletteImage(t, []color.NRGBA{{255, 0, 0, 0}})); err == nil {
		t.Fatal("transparent image accepted")
	}
	two, err := ExtractPalette(paletteImage(t, []color.NRGBA{{0, 255, 0, 255}, {255, 0, 0, 255}}))
	if err != nil || two.Accent == two.BG || two.Accent == two.Highlight {
		t.Fatal("two-colour derivation", two, err)
	}

	if _, err := ExtractPalette([]byte("not an image")); err == nil {
		t.Fatal("invalid image accepted")
	}
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	img.SetNRGBA(2, 2, color.NRGBA{255, 0, 0, 255})
	var b bytes.Buffer
	for _, encode := range []func() error{func() error { return jpeg.Encode(&b, img, nil) }, func() error { return gif.Encode(&b, img, nil) }} {
		b.Reset()
		if err := encode(); err != nil {
			t.Fatal(err)
		}
		if _, err := ExtractPalette(b.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	// DecodeConfig catches a decompression bomb before allocation.
	img = image.NewNRGBA(image.Rect(0, 0, 5000, 4100))
	b.Reset()
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractPalette(b.Bytes()); err == nil {
		t.Fatal("oversized raster accepted")
	}
	if _, err := ExtractPalette(make([]byte, maxPaletteBytes+1)); err == nil {
		t.Fatal("oversized file accepted")
	}
}
func TestPalettesAPI(t *testing.T) {
	h := start(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	f, _ := mw.CreateFormFile("file", "暖色.png")
	f.Write(paletteImage(t, []color.NRGBA{{240, 230, 210, 255}, {10, 20, 30, 255}}))
	mw.Close()
	resp, err := h.client.Post(h.url("/api/palettes/analyze"), mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	var analyzed struct {
		Palette PaletteDraft `json:"palette"`
	}
	json.NewDecoder(resp.Body).Decode(&analyzed)
	resp.Body.Close()
	if resp.StatusCode != 200 || analyzed.Palette.Name != "暖色" {
		t.Fatal(resp.StatusCode, analyzed)
	}
	code, body := h.do("GET", "/api/palettes", nil)
	if code != 200 || !strings.Contains(string(body), `"palettes":[]`) {
		t.Fatal("analysis saved data", string(body))
	}
	code, body = h.do("POST", "/api/palettes", analyzed.Palette)
	if code != 201 {
		t.Fatal(code, string(body))
	}
	var out struct {
		Palette Palette `json:"palette"`
	}
	json.Unmarshal(body, &out)
	id := out.Palette.ID
	if id == "" {
		t.Fatal(string(body))
	}
	code, body = h.do("PATCH", "/api/palettes/"+id, map[string]string{"name": "冷色", "accent": "#AABBCC"})
	if code != 200 {
		t.Fatal(code, string(body))
	}
	json.Unmarshal(body, &out)
	if out.Palette.Name != "冷色" || out.Palette.Accent != "#aabbcc" || out.Palette.BG != analyzed.Palette.BG {
		t.Fatal(out)
	}
	for _, patch := range []any{map[string]any{"text": nil}, map[string]any{"bg": "red", "name": "不应保存"}, map[string]any{"extra": "#ffffff"}, map[string]string{}, map[string]string{"name": " "}} {
		code, body = h.do("PATCH", "/api/palettes/"+id, patch)
		if code != 400 {
			t.Fatal("invalid patch", code, string(body))
		}
	}
	code, body = h.do("GET", "/api/palettes/"+id, nil)
	json.Unmarshal(body, &out)
	if code != 200 || out.Palette.Name != "冷色" {
		t.Fatal("invalid patch changed name")
	}
	for _, payload := range []any{map[string]string{"name": "缺少颜色"}, map[string]any{"name": "无效", "bg": "#fff"}, nil} {
		code, _ = h.do("POST", "/api/palettes", payload)
		if code != 400 {
			t.Fatal("invalid create", code)
		}
	}
	project := h.project("配色测试")
	// HTTP apply and ordinary commands share the same undo stack.
	before, err := h.srv.engine(project)
	if err != nil {
		t.Fatal(err)
	}
	original := before.Snapshot()
	code, body = h.do("POST", "/api/projects/"+project+"/palettes/"+id+"/apply", map[string]string{"board": "1"})
	if code != 200 {
		t.Fatal(code, string(body))
	}
	var result Result
	json.Unmarshal(body, &result)
	if !result.OK || !result.History.CanUndo {
		t.Fatal(string(body))
	}
	parsed, _ := doc.Parse(result.Document)
	if parsed.ActiveBoard().Canvas.Background != out.Palette.BG {
		t.Fatal("apply background")
	}
	h.do("POST", "/api/projects/"+project+"/commands", map[string]string{"type": "undo"})
	if !bytes.Equal(original, before.Snapshot()) {
		t.Fatal("HTTP apply did not undo")
	}
	// A second project can use the same global palette.
	second := h.project("共享测试")
	code, body = h.do("POST", "/api/projects/"+second+"/commands", map[string]string{"type": "applyPalette", "id": id})
	if code != 200 {
		t.Fatal(code, string(body))
	}
	code, body = h.do("GET", "/api/schema", nil)
	var schema struct {
		Endpoints []paletteRoute `json:"endpoints"`
		Commands  []struct {
			Type string `json:"type"`
		} `json:"commands"`
	}
	json.Unmarshal(body, &schema)
	if code != 200 || len(schema.Endpoints) != len(paletteRoutes) {
		t.Fatal("missing REST schema")
	}
	found := false
	for _, c := range schema.Commands {
		found = found || c.Type == "applyPalette"
	}
	if !found {
		t.Fatal("missing command schema")
	}
	for _, r := range schema.Endpoints {
		if !strings.HasPrefix(r.Path, "/api/") {
			t.Fatal(r.Path)
		}
	}
	code, body = h.do("DELETE", "/api/palettes/"+id, nil)
	if code != 200 {
		t.Fatal(code, string(body))
	}
	e, _ := h.srv.engine(second)
	if e.Project().ActiveBoard().Canvas.Background != out.Palette.BG {
		t.Fatal("delete changed applied document")
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPatch} {
		code, _ = h.do(method, "/api/palettes/"+id, map[string]string{"name": "test"})
		if code != 404 {
			t.Fatal(method, code)
		}
	}
	code, _ = h.do("POST", "/api/projects/"+project+"/palettes/"+id+"/apply", map[string]string{})
	if code != 404 {
		t.Fatal(code)
	}
}
func TestPaletteStorageReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.AddPalette(PaletteDraft{Name: "持久化", PaletteColors: doc.PaletteColors{BG: "#ffffff", Text: "#000000", Highlight: "#ff0000", Text2: "#666666", Accent: "#00aa00"}})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Palette(p.ID)
	if err != nil || !reflect.DeepEqual(got, p) {
		t.Fatal(got, err)
	}
}

func (h *harness) paletteUpload(method, path string, metadata any, raw []byte) (int, []byte) {
	h.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	meta, _ := json.Marshal(metadata)
	mw.WriteField("palette", string(meta))
	file, _ := mw.CreateFormFile("file", "misleading-name.jpg")
	file.Write(raw)
	mw.Close()
	req, _ := http.NewRequest(method, h.url(path), &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func TestPaletteOriginalImage(t *testing.T) {
	h := start(t)
	raw := paletteImage(t, []color.NRGBA{{230, 240, 250, 255}})
	colors, _ := ExtractPalette(raw)
	draft := PaletteDraft{Name: "原图", PaletteColors: colors}
	draft.BG = "#123456" // explicit overrides must not lose the original
	count := func(want int) {
		t.Helper()
		files, err := os.ReadDir(filepath.Join(h.srv.DataDir(), "assets"))
		if err != nil || len(files) != want {
			t.Fatalf("assets: %d, want %d: %v", len(files), want, err)
		}
	}
	// Invalid metadata or images must not create files or palette records.
	for _, data := range []struct {
		metadata any
		image    []byte
	}{
		{map[string]string{"name": "bad"}, raw},
		{draft, []byte("not an image")},
	} {
		status, body := h.paletteUpload("POST", "/api/palettes", data.metadata, data.image)
		if status != 400 {
			t.Fatal(status, string(body))
		}
		count(0)
	}
	status, body := h.paletteUpload("POST", "/api/palettes", draft, raw)
	var out struct {
		Palette Palette `json:"palette"`
	}
	json.Unmarshal(body, &out)
	p := out.Palette
	if status != 201 || p.BG != draft.BG || p.ImageURL != "/assets/"+sha1Hex(raw)+".png" {
		t.Fatal(status, string(body))
	}
	count(1)
	status, body = h.do("GET", p.ImageURL, nil)
	if status != 200 || !bytes.Equal(raw, body) {
		t.Fatal("original bytes changed", status)
	}
	for _, path := range []string{"/api/palettes", "/api/palettes/" + p.ID} {
		status, body = h.do("GET", path, nil)
		if status != 200 || !bytes.Contains(body, []byte(p.ImageURL)) {
			t.Fatal("imageUrl missing", string(body))
		}
	}
	// Identical uploads deduplicate; replacing a shared image does not delete it.
	status, body = h.paletteUpload("POST", "/api/palettes", draft, raw)
	if status != 201 {
		t.Fatal(status, string(body))
	}
	count(1)
	replacement := paletteImage(t, []color.NRGBA{{20, 50, 80, 255}})
	status, body = h.paletteUpload("PATCH", "/api/palettes/"+p.ID, map[string]string{}, replacement)
	json.Unmarshal(body, &out)
	if status != 200 || out.Palette.ImageURL == p.ImageURL || out.Palette.PaletteColors != p.PaletteColors || out.Palette.Name != p.Name {
		t.Fatal(status, string(body))
	}
	count(2)
	status, body = h.do("GET", out.Palette.ImageURL, nil)
	if status != 200 || !bytes.Equal(body, replacement) {
		t.Fatal("replacement bytes", status)
	}
	status, body = h.do("GET", p.ImageURL, nil)
	if status != 200 || !bytes.Equal(body, raw) {
		t.Fatal("shared original deleted")
	}
	for _, patch := range []map[string]string{{"imageUrl": "https://example.com/x.png"}, {"imageUrl": "/assets/../../x.png"}, {"imageUrl": "/assets/" + strings.Repeat("0", 40) + ".png"}} {
		status, _ = h.do("PATCH", "/api/palettes/"+p.ID, patch)
		if status != 400 {
			t.Fatal("invalid image reference accepted", patch)
		}
	}
	unique := paletteImage(t, []color.NRGBA{{120, 50, 80, 255}})
	status, _ = h.paletteUpload("PATCH", "/api/palettes/"+p.ID, map[string]string{"bg": "bad"}, unique)
	if status != 400 {
		t.Fatal(status)
	}
	count(2)
	status, _ = h.paletteUpload("PATCH", "/api/palettes/missing", map[string]string{}, unique)
	if status != 404 {
		t.Fatal(status)
	}
	count(2)
	_, body = h.do("GET", "/api/schema", nil)
	if !bytes.Contains(body, []byte("imageUrl")) || !bytes.Contains(body, []byte("multipart")) {
		t.Fatal("schema missing images")
	}
}

func TestPaletteImageMigration(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.db.Exec(`DROP TABLE palettes;
	CREATE TABLE palettes (id TEXT PRIMARY KEY, name TEXT NOT NULL, bg TEXT NOT NULL, text TEXT NOT NULL, highlight TEXT NOT NULL, text2 TEXT NOT NULL, accent TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
	INSERT INTO palettes VALUES ('pal_old','旧配色','#ffffff','#000000','#ff0000','#777777','#00ff00',1,1);`)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	for i := 0; i < 2; i++ {
		store, err = OpenStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		p, err := store.Palette("pal_old")
		if err != nil || p.ImageURL != "" || p.Name != "旧配色" {
			t.Fatal(p, err)
		}
		store.Close()
	}
}

func TestPaletteReusesLibraryOriginal(t *testing.T) {
	for _, ext := range []string{"jpg", "bin"} {
		t.Run(ext, func(t *testing.T) {
			h := start(t)
			raw := paletteImage(t, []color.NRGBA{{120, 150, 180, 255}})
			if ext == "jpg" {
				img, _, _ := image.Decode(bytes.NewReader(raw))
				var jpegBytes bytes.Buffer
				if err := jpeg.Encode(&jpegBytes, img, nil); err != nil {
					t.Fatal(err)
				}
				raw = jpegBytes.Bytes()
			}
			asset, err := h.srv.assets.SaveBytes(raw, ext)
			if err != nil {
				t.Fatal(err)
			}
			colors, _ := ExtractPalette(raw)
			draft := PaletteDraft{Name: "素材原图", PaletteColors: colors}
			status, body := h.paletteUpload("POST", "/api/palettes", draft, raw)
			var out struct {
				Palette Palette `json:"palette"`
			}
			json.Unmarshal(body, &out)
			if status != 201 {
				t.Fatal(status, string(body))
			}
			if ext == "jpg" && out.Palette.ImageURL != "/assets/"+asset.SHA+".jpg" {
				t.Fatal("existing image reference not reused", out)
			}
			status, body = h.do("GET", out.Palette.ImageURL, nil)
			if status != 200 || !bytes.Equal(body, raw) {
				t.Fatal("original not available")
			}
		})
	}
}
