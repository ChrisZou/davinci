package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/url"
	"testing"
)

func (h *harness) projectFromImage(fields map[string]string, file []byte) (int, map[string]any) {
	h.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("file", "cover.png")
	_, _ = fw.Write(file)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", h.url("/api/projects/from-image"), &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatalf("from-image: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (h *harness) listProjects(query string) []ProjectSummary {
	h.t.Helper()
	code, body := h.do("GET", "/api/projects"+query, nil)
	var out struct {
		Projects []ProjectSummary `json:"projects"`
	}
	_ = json.Unmarshal(body, &out)
	if code != http.StatusOK {
		h.t.Fatalf("list projects%s: HTTP %d: %s", query, code, body)
	}
	return out.Projects
}

// A template is a project filed in the template library: it stays out of the
// works listing, is found by tag and by words in its note, and moves between
// the two libraries with a PATCH.
func TestTemplatesAreProjectsOfAnotherKind(t *testing.T) {
	h := start(t)
	work := h.project("我的封面")

	code, out := h.projectFromImage(map[string]string{
		"name": "大字报风", "tags": "大字,红底白字", "note": "标题压满上半屏，人物压在字上", "link": "https://www.xiaohongshu.com/explore/abc",
	}, testPNG(t, 621, 828))
	if code != http.StatusCreated {
		t.Fatalf("from-image: HTTP %d: %v", code, out)
	}
	tpl := out["project"].(map[string]any)
	id := tpl["id"].(string)
	if tpl["kind"] != KindTemplate || tpl["width"].(float64) != 621 || tpl["height"].(float64) != 828 {
		t.Fatalf("template = %v", tpl)
	}
	// The picture is an ordinary layer filling the canvas.
	if l := h.layers(id); len(l) != 1 || l[0].Type != "image" {
		t.Fatalf("layers = %+v", l)
	}

	if l := h.listProjects(""); len(l) != 1 || l[0].ID != work {
		t.Errorf("works = %+v, want just the work", l)
	}
	if l := h.listProjects("?kind=template"); len(l) != 1 || l[0].ID != id || len(l[0].Tags) != 2 {
		t.Errorf("templates = %+v", l)
	}
	if l := h.listProjects("?kind=all"); len(l) != 2 {
		t.Errorf("all = %d, want 2", len(l))
	}
	if l := h.listProjects("?kind=template&q=" + url.QueryEscape("人物 上半屏")); len(l) != 1 {
		t.Errorf("search note = %d, want 1", len(l))
	}
	// A tag filter matches whole tags: 红底 is part of 红底白字, not a tag.
	if l := h.listProjects("?kind=template&tag=" + url.QueryEscape("红底")); len(l) != 0 {
		t.Errorf("tag 红底 = %d, want 0", len(l))
	}

	// Filing the work as a template too.
	code, body := h.do("PATCH", "/api/projects/"+work, map[string]any{"kind": "template", "tags": []string{"大字", " 大字 ", "对比色"}, "note": "自己的也留一份"})
	if code != http.StatusOK {
		t.Fatalf("patch: HTTP %d: %s", code, body)
	}
	if l := h.listProjects(""); len(l) != 0 {
		t.Errorf("works after the move = %d, want 0", len(l))
	}
	code, body = h.do("GET", "/api/projects/tags", nil)
	var tags struct {
		Tags []TagCount `json:"tags"`
	}
	_ = json.Unmarshal(body, &tags)
	if code != http.StatusOK || len(tags.Tags) != 3 || tags.Tags[0].Name != "大字" || tags.Tags[0].Count != 2 {
		t.Errorf("tags = %+v (HTTP %d)", tags.Tags, code)
	}
	if code, _ := h.do("PATCH", "/api/projects/"+work, map[string]any{"kind": "folder"}); code != http.StatusBadRequest {
		t.Errorf("bad kind: HTTP %d, want 400", code)
	}
}

// Starting a design from a template copies the whole layered document into a
// new work and leaves the template as it was.
func TestDuplicateTemplateIntoDesign(t *testing.T) {
	h := start(t)
	_, out := h.projectFromImage(map[string]string{"name": "参考封面", "tags": "大字", "note": "备注"}, testPNG(t, 600, 800))
	id := out["project"].(map[string]any)["id"].(string)
	if ok, msg := h.exec(id, map[string]any{"type": "addText", "text": "标题"}); !ok {
		t.Fatalf("addText: %s", msg)
	}

	code, body := h.do("POST", "/api/projects/"+id+"/duplicate", map[string]any{"name": "我的新封面"})
	var res struct {
		Project ProjectSummary `json:"project"`
	}
	_ = json.Unmarshal(body, &res)
	if code != http.StatusCreated {
		t.Fatalf("duplicate: HTTP %d: %s", code, body)
	}
	p := res.Project
	if p.Kind != KindDesign || p.Name != "我的新封面" || p.Width != 600 || len(p.Tags) != 0 || p.Note != "" {
		t.Fatalf("copy = %+v", p)
	}
	if l := h.layers(p.ID); len(l) != 2 {
		t.Fatalf("copy has %d layers, want 2", len(l))
	}
	// Editing the copy leaves the template alone.
	if ok, msg := h.exec(p.ID, map[string]any{"type": "addText", "text": "再加一行"}); !ok {
		t.Fatalf("addText on copy: %s", msg)
	}
	if l := h.layers(id); len(l) != 2 {
		t.Errorf("template has %d layers after editing the copy, want 2", len(l))
	}

	// Copying within the template library keeps the tags and note.
	code, body = h.do("POST", "/api/projects/"+id+"/duplicate", map[string]any{"kind": "template"})
	_ = json.Unmarshal(body, &res)
	if code != http.StatusCreated || res.Project.Kind != KindTemplate || res.Project.Name != "参考封面" || res.Project.Note != "备注" {
		t.Errorf("template copy: HTTP %d, %+v", code, res.Project)
	}
}

func TestFitLongSide(t *testing.T) {
	for _, c := range []struct{ w, h, ww, wh int }{
		{800, 500, 800, 500},
		{6000, 2000, 3000, 1000},
		{1000, 4500, 667, 3000},
	} {
		if w, h := fitLongSide(c.w, c.h, 3000); w != c.ww || h != c.wh {
			t.Errorf("fitLongSide(%d, %d) = %d×%d, want %d×%d", c.w, c.h, w, h, c.ww, c.wh)
		}
	}
}
