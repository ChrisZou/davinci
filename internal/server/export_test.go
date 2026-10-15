package server

import (
	"bytes"
	"davinci/internal/doc"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/labstack/echo/v4"
)

type exportService struct {
	doc.Services
	options []doc.RenderOptions
	boards  []string
}

func (s *exportService) Render(b *doc.Board, opts doc.RenderOptions) ([]byte, error) {
	s.options = append(s.options, opts)
	s.boards = append(s.boards, b.ID)
	var buf bytes.Buffer
	err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 3)))
	return buf.Bytes(), err
}
func (s *exportService) Measure([]*doc.Layer) (map[string]doc.TextSize, error) { return nil, nil }

func TestExportHTTPOptions(t *testing.T) {
	// Use an engine with a recording renderer, so the HTTP contract can be tested
	// even in builds without the embedded CanvasKit assets.
	h := start(t)
	id := h.project("export-test")
	p := doc.Blank(20, 10)
	p.Boards[0].ID = "b1"
	p.Boards[0].Layers = []*doc.Layer{{ID: "one", Name: "标题", Type: "shape", Width: 4, Height: 3}}
	p.Boards = append(p.Boards, &doc.Board{ID: "b2", Name: "second", Canvas: doc.Canvas{Width: 30, Height: 15}})
	p.Active = "b2"
	svc := &exportService{}
	h.srv.eng.mu.Lock()
	h.srv.eng.byID[id] = doc.NewEngine(p, svc)
	h.srv.eng.mu.Unlock()
	// Invoke the route directly to inspect MIME without hiding it in h.do.
	request := func(ext, query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/export."+ext+query, nil)
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)
		c.SetParamNames("id", "ext")
		c.SetParamValues(id, ext)
		if err := h.srv.handleExport(c); err != nil {
			t.Fatal(err)
		}
		return rec
	}
	r := request("png", "?board=1&scale=3&quality=0.7&transparent=true&ids=标题")
	if r.Code != 200 || r.Header().Get("Content-Type") != "image/png" {
		t.Fatal(r.Code, r.Body.String())
	}
	opts := svc.options[0]
	if opts.Scale != 3 || opts.Quality != 0.7 || !opts.Transparent || !reflect.DeepEqual(opts.IDs, []string{"one"}) || svc.boards[0] != "b1" {
		t.Fatal(opts, svc.boards)
	}
	r = request("zip", "?format=webp&scale=2")
	if r.Code != 200 || r.Header().Get("Content-Type") != "application/zip" || len(svc.options) != 3 || svc.options[2].Format != "webp" {
		t.Fatal(r.Code, r.Body.String())
	}
	r = request("png", "?allBoards=true")
	if r.Code != 200 || r.Header().Get("Content-Type") != "application/zip" {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, q := range []string{"?scale=NaN", "?scale=Inf", "?quality=NaN", "?quality=2", "?transparent=nope", "?allBoards=yes", "?allBoards=true&board=1", "?allBoards=true&ids=标题", "?ids=missing"} {
		if r := request("png", q); r.Code != 400 {
			t.Fatalf("%s: %d %s", q, r.Code, r.Body.String())
		}
	}
	if r := request("gif", ""); r.Code != 400 {
		t.Fatal(r.Code)
	}
	if r := request("zip", "?format=gif"); r.Code != 400 {
		t.Fatal(r.Code)
	}
}
