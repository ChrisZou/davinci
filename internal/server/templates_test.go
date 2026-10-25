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
	_, out := h.projectFromImage(map[string]string{"name": "参考封面", "tags": "大字", "note": "备注", "source": "小红书", "author": "作者", "likes": "125", "link": "https://example.com/post"}, testPNG(t, 600, 800))
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
	if p.Kind != KindDesign || p.Name != "我的新封面" || p.Width != 600 || len(p.Tags) != 0 || p.Note != "" || p.Link != "" || p.Source != "" || p.Author != "" || p.Likes != 0 || p.TemplateID != id {
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
	res.Project = ProjectSummary{}
	_ = json.Unmarshal(body, &res)
	if code != http.StatusCreated || res.Project.Kind != KindTemplate || res.Project.Name != "参考封面" || res.Project.Note != "备注" || res.Project.Source != "小红书" || res.Project.Author != "作者" || res.Project.Likes != 125 || res.Project.Link != "https://example.com/post" || res.Project.TemplateID != "" {
		t.Errorf("template copy: HTTP %d, %+v", code, res.Project)
	}
}

func TestProjectProvenanceAPI(t *testing.T) {
	h := start(t)
	fields := map[string]any{"name": "来源测试", "kind": KindTemplate, "source": " 小红书 ", "author": " 作者甲 ", "likes": 1234, "templateId": " p_original ", "link": "https://example.com/post", "note": "备注"}
	code, body := h.do("POST", "/api/projects", fields)
	var out struct {
		Project Project `json:"project"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	id := out.Project.ID
	check := func(method, path string, payload any) {
		t.Helper()
		code, body := h.do(method, path, payload)
		var res struct {
			Project Project `json:"project"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			t.Fatal(err)
		}
		p := res.Project
		if code != http.StatusOK || p.Source != "小红书" || p.Author != "作者甲" || p.Likes != 1234 || p.TemplateID != "p_original" || p.Link != fields["link"] || p.Note != "备注" {
			t.Fatalf("%s: %d %s", method, code, body)
		}
	}
	check("GET", "/api/projects/"+id, nil)
	check("PATCH", "/api/projects/"+id, map[string]any{"source": "小红书", "author": "作者甲", "likes": 1234, "templateId": "p_original"})
	check("PATCH", "/api/projects/"+id, map[string]any{"tags": []string{"大字"}})
	list := h.listProjects("?kind=template&q=" + url.QueryEscape("小红书 作者甲"))
	if len(list) != 1 || list[0].Source != "小红书" || list[0].Author != "作者甲" || list[0].Likes != 1234 || list[0].TemplateID != "p_original" {
		t.Fatalf("list: %+v", list)
	}
	for _, likes := range []any{-1, 1.5, "10"} {
		for _, route := range []struct{ method, path string }{{"PATCH", "/api/projects/" + id}, {"POST", "/api/projects"}, {"POST", "/api/projects/from-image"}} {
			code, body := h.do(route.method, route.path, map[string]any{"likes": likes, "name": "不应改名", "ref": "/missing.png"})
			if code != http.StatusBadRequest {
				t.Fatalf("invalid likes %v: %d %s", likes, code, body)
			}
		}
	}
	check("GET", "/api/projects/"+id, nil)
	for _, raw := range []string{"-1", "1.5", "oops"} {
		code, out := h.projectFromImage(map[string]string{"likes": raw}, testPNG(t, 1, 1))
		if code != http.StatusBadRequest {
			t.Fatalf("multipart likes %q: %d %+v", raw, code, out)
		}
	}
	code, body = h.do("PATCH", "/api/projects/"+id, map[string]any{"source": "", "author": "", "likes": 0, "templateId": ""})
	if code != http.StatusOK {
		t.Fatalf("clear: %d %s", code, body)
	}
	code, body = h.do("GET", "/api/projects/"+id, nil)
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if code != http.StatusOK || out.Project.Source != "" || out.Project.Author != "" || out.Project.Likes != 0 || out.Project.TemplateID != "" || out.Project.Name != "来源测试" || out.Project.Note != "备注" {
		t.Fatalf("clear: %d %s", code, body)
	}
	code, body = h.do("GET", "/api/schema", nil)
	var schema struct {
		ProjectMetadata map[string]map[string]any `json:"projectMetadata"`
	}
	if err := json.Unmarshal(body, &schema); err != nil {
		t.Fatal(err)
	}
	if code != http.StatusOK || schema.ProjectMetadata["source"]["label"] != "来源" || schema.ProjectMetadata["link"]["label"] != "来源链接" || schema.ProjectMetadata["likes"]["minimum"] != float64(0) {
		t.Fatalf("schema: %d %s", code, body)
	}
}

func TestProjectFromImageJSONMetadata(t *testing.T) {
	h := start(t)
	asset, err := h.srv.assets.SaveBytes(testPNG(t, 10, 20), "png")
	if err != nil {
		t.Fatal(err)
	}
	code, body := h.do("POST", "/api/projects/from-image", map[string]any{"ref": "/assets/" + asset.SHA + "." + asset.Ext, "source": "B站", "author": "作者乙", "likes": 0, "templateId": "p_template"})
	var out struct {
		Project ProjectSummary `json:"project"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if code != http.StatusCreated || out.Project.Source != "B站" || out.Project.Author != "作者乙" || out.Project.Likes != 0 || out.Project.TemplateID != "p_template" {
		t.Fatalf("from-image JSON: %d %s", code, body)
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
