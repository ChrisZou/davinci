package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The invariants a browser proves while you watch it, restated so a silent
// regression fails `go test` instead. These are the ones that have already
// bitten: a broken image reference quietly becoming a black rectangle, and a
// command that cannot be executed answering "ok" anyway.

type harness struct {
	t      *testing.T
	srv    *Server
	client *http.Client
}

func start(t *testing.T) *harness {
	t.Helper()
	// A port nobody holds: Port 0 means "the default 7789" to the server, which
	// collides with a davinci the developer has running.
	srv, err := NewServer(Options{Port: freePort(t), DataDir: t.TempDir(), Quiet: true})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return &harness{t: t, srv: srv, client: &http.Client{Timeout: 10 * time.Second}}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func (h *harness) url(path string) string { return h.srv.Addr() + path }

func (h *harness) do(method, path string, payload any) (int, []byte) {
	h.t.Helper()
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			h.t.Fatalf("marshal: %v", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, h.url(path), body)
	if err != nil {
		h.t.Fatalf("request: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (h *harness) project(name string) string {
	h.t.Helper()
	code, body := h.do("POST", "/api/projects", map[string]any{"name": name})
	if code != http.StatusOK && code != http.StatusCreated {
		h.t.Fatalf("create project: HTTP %d: %s", code, body)
	}
	var out struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		h.t.Fatalf("create project parse: %v", err)
	}
	return out.Project.ID
}

// layers returns the layer names as `/api/projects/:id/layers` reports them,
// front-to-back — the order an AI reasons about, and the one the editor's layer
// panel must agree with.
func (h *harness) layers(project string) []layerRow {
	h.t.Helper()
	code, body := h.do("GET", "/api/projects/"+project+"/layers", nil)
	if code != http.StatusOK {
		h.t.Fatalf("layers: HTTP %d: %s", code, body)
	}
	var out struct {
		Layers []layerRow `json:"layers"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		h.t.Fatalf("layers parse: %v", err)
	}
	return out.Layers
}

type layerRow struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Preview string `json:"preview"`
}

// putDocument replaces a project's document wholesale, which is how an import or
// another tab hands one over.
func (h *harness) putDocument(project string, doc map[string]any) {
	h.t.Helper()
	code, body := h.do("PUT", "/api/projects/"+project, doc)
	if code != http.StatusOK {
		h.t.Fatalf("put document: HTTP %d: %s", code, body)
	}
}

// exec posts a command and reports whether the server accepted it.
func (h *harness) exec(project string, cmd map[string]any) (bool, string) {
	h.t.Helper()
	code, body := h.do("POST", "/api/projects/"+project+"/commands", cmd)
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &out)
	return out.OK && code == http.StatusOK, out.Error
}

func names(rows []layerRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out
}

// An image layer whose URL is gone must stay an image in the document. Treating
// it as a shape turns it into a black rectangle, and the next save persists the
// loss — the picture is gone and nothing says so.
func TestBrokenImageSurvivesAnImport(t *testing.T) {
	h := start(t)
	id := h.project("坏图")
	h.putDocument(id, map[string]any{
		"version": 1,
		"canvas":  map[string]any{"width": 800, "height": 600},
		"layers": []map[string]any{
			{"id": "i_broken", "name": "坏图", "type": "image", "x": 10, "y": 10,
				"width": 300, "height": 200, "image": map[string]any{"url": ""}},
		},
	})

	rows := h.layers(id)
	if len(rows) != 1 {
		t.Fatalf("want 1 layer, got %v", names(rows))
	}
	if rows[0].Type != "image" {
		t.Errorf("a missing URL is a broken reference, not a shape: type = %q, want image", rows[0].Type)
	}
}

// A command the server cannot carry out must be refused, not answered "ok".
// The refusal is what makes an AI stop and ask instead of walking on: an "ok"
// for a picture that never loaded leaves the caller believing it is repaired.
func TestAnUnrunnableCommandIsRefused(t *testing.T) {
	h := start(t)
	id := h.project("没人执行")
	h.putDocument(id, map[string]any{
		"version": 1,
		"canvas":  map[string]any{"width": 800, "height": 600},
		"layers": []map[string]any{
			{"id": "i_broken", "name": "坏图", "type": "image", "x": 10, "y": 10,
				"width": 300, "height": 200, "image": map[string]any{"url": ""}},
		},
	})

	// The picture cannot be fetched, so the server cannot replace it.
	ok, msg := h.exec(id, map[string]any{"type": "replaceImage", "id": "i_broken", "url": "http://127.0.0.1:1/never.png"})
	if ok {
		t.Error("a command nobody can execute answered ok")
	}
	if msg == "" {
		t.Error("the refusal carried no reason, so there is nothing for the caller to act on")
	}
	rows := h.layers(id)
	if len(rows) != 1 || rows[0].Type != "image" {
		t.Errorf("a refused command must not disturb the stored layer: %v", names(rows))
	}
}

// The layer list an AI reads is front-to-back, and its first row is the one
// drawn last (on top). Reversing it silently tells AI the opposite of the truth
// about every z-index it picks.
func TestLayerRowsAreFrontToBack(t *testing.T) {
	h := start(t)
	id := h.project("顺序")
	h.putDocument(id, map[string]any{
		"version": 1,
		"canvas":  map[string]any{"width": 800, "height": 600},
		"layers": []map[string]any{
			{"id": "l_back", "name": "底层", "type": "shape", "x": 0, "y": 0, "width": 100, "height": 100,
				"shape": map[string]any{"kind": "rect"}, "style": map[string]any{"fill": "#111111"}},
			{"id": "l_mid", "name": "中层", "type": "shape", "x": 0, "y": 0, "width": 100, "height": 100,
				"shape": map[string]any{"kind": "rect"}, "style": map[string]any{"fill": "#222222"}},
			{"id": "l_front", "name": "顶层", "type": "text", "x": 0, "y": 0, "width": 100, "height": 40, "text": "顶"},
		},
	})

	got := names(h.layers(id))
	want := []string{"顶层", "中层", "底层"} // front first
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("layer rows = %v, want %v (front-to-back)", got, want)
	}
}

// Uploading an asset must hand back a URL the canvas can fetch, and the bytes
// must come back unchanged — AI asks for a picture, not a placeholder.
func TestAssetUploadRoundTrips(t *testing.T) {
	h := start(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "dot.png")
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	// A 1×1 transparent PNG.
	payload := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00,
		0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T', 0x78, 0x9c,
		0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4,
		0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
	if _, err := fw.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	req, err := http.NewRequest("POST", h.url("/api/assets"), &buf)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload: HTTP %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Asset struct {
			URL string `json:"url"`
		} `json:"asset"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("upload parse: %v", err)
	}
	if !strings.HasPrefix(out.Asset.URL, "/assets/") {
		t.Fatalf("asset URL = %q, want one under /assets/", out.Asset.URL)
	}

	got, err := h.client.Get(h.url(out.Asset.URL))
	if err != nil {
		t.Fatalf("fetch asset: %v", err)
	}
	served, _ := io.ReadAll(got.Body)
	got.Body.Close()
	if !bytes.Equal(served, payload) {
		t.Errorf("asset came back as %d bytes, want the %d we uploaded", len(served), len(payload))
	}
}

// Renaming a project must not touch its document, and an empty name is refused
// rather than stored.
func TestProjectRenameKeepsTheDocument(t *testing.T) {
	h := start(t)
	id := h.project("旧名字")
	_, before := h.do("GET", "/api/projects/"+id, nil)

	if code, body := h.do("PATCH", "/api/projects/"+id, map[string]any{"name": "  新名字  "}); code != http.StatusOK {
		t.Fatalf("rename: HTTP %d: %s", code, body)
	}
	var got struct {
		Project struct {
			Name     string          `json:"name"`
			Document json.RawMessage `json:"document"`
		} `json:"project"`
	}
	_, after := h.do("GET", "/api/projects/"+id, nil)
	if err := json.Unmarshal(after, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Project.Name != "新名字" {
		t.Fatalf("name = %q, want the trimmed new name", got.Project.Name)
	}
	var was struct {
		Project struct {
			Document json.RawMessage `json:"document"`
		} `json:"project"`
	}
	_ = json.Unmarshal(before, &was)
	if !bytes.Equal(was.Project.Document, got.Project.Document) {
		t.Fatalf("rename changed the document")
	}

	if code, _ := h.do("PATCH", "/api/projects/"+id, map[string]any{"name": "   "}); code != http.StatusBadRequest {
		t.Fatalf("empty name: HTTP %d, want 400", code)
	}
	if code, _ := h.do("PATCH", "/api/projects/p_nope", map[string]any{"name": "x"}); code != http.StatusNotFound {
		t.Fatalf("unknown project: HTTP %d, want 404", code)
	}
}

// The project list shows thumbnails as plain images: a project nothing has
// saved yet has none (404), and a saved one comes back as image bytes.
func TestThumbnailIsServedAsAnImage(t *testing.T) {
	h := start(t)
	id := h.project("缩略图")
	if code, _ := h.do("GET", "/api/projects/"+id+"/thumbnail", nil); code != http.StatusNotFound {
		t.Fatalf("no thumbnail yet: HTTP %d, want 404", code)
	}

	// A 1×1 PNG, stored the way the thumbnail renderer stores it: a data URL.
	const png1x1 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="
	doc := json.RawMessage(`{"version":1,"canvas":{"width":10,"height":10},"layers":[]}`)
	if _, err := h.srv.store.SaveDocument(id, doc, 10, 10); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := h.srv.store.SetThumbnail(id, "data:image/png;base64,"+png1x1); err != nil {
		t.Fatalf("thumbnail: %v", err)
	}
	resp, err := h.client.Get(h.url("/api/projects/" + id + "/thumbnail?rev=1"))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("thumbnail: HTTP %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content type = %q, want image/png", ct)
	}
	if !bytes.HasPrefix(body, []byte("\x89PNG")) {
		t.Fatalf("body is not a PNG")
	}
}

// DAVINCI_DATA wins; without it and outside a checkout, data goes under the
// current directory — never into the home folder.
func TestDefaultDataDir(t *testing.T) {
	t.Setenv("DAVINCI_DATA", "/tmp/somewhere")
	if got := DefaultDataDir(); got != "/tmp/somewhere" {
		t.Fatalf("with DAVINCI_DATA: %q", got)
	}
	t.Setenv("DAVINCI_DATA", "")
	home, _ := os.UserHomeDir()
	got := DefaultDataDir()
	if filepath.Base(got) != "data" || filepath.Dir(got) == home {
		t.Fatalf("default = %q, want a data/ dir that is not directly in home", got)
	}
}

// The default port taken (by a davinci the developer runs, or held here) is no
// reason to fail: the server takes another and says where in server.json. A
// port asked for is not swapped; a second server on the same data directory is
// refused; and server.json goes away with the server.
func TestPortFallbackServerFileAndLock(t *testing.T) {
	if l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", DefaultPort)); err == nil {
		defer l.Close()
	}
	dir := t.TempDir()
	srv, err := NewServer(Options{DataDir: dir, Quiet: true})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if srv.Port() == DefaultPort {
		t.Fatal("took the busy default port")
	}
	f, err := ReadServerFile(dir)
	if err != nil || f.URL != srv.Addr() || f.Boot != srv.BootID() || f.PID != os.Getpid() {
		t.Fatalf("server.json = %+v, %v; want %s", f, err, srv.Addr())
	}
	if resp, err := http.Get(f.URL + "/api/health"); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("health at %s: %v", f.URL, err)
	}

	if _, err := NewServer(Options{DataDir: dir, Quiet: true}); err == nil || !strings.Contains(err.Error(), f.URL) {
		t.Fatalf("a second server on the same data directory: %v", err)
	}
	strict, err := NewServer(Options{Port: srv.Port(), DataDir: t.TempDir(), Quiet: true})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := strict.Start(); err == nil {
		t.Fatal("a port asked for and taken should fail, not be swapped")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	if _, err := os.Stat(ServerFilePath(dir)); !os.IsNotExist(err) {
		t.Fatalf("server.json left behind: %v", err)
	}
	again, err := NewServer(Options{DataDir: dir, Quiet: true})
	if err != nil {
		t.Fatalf("the data directory should be free again: %v", err)
	}
	_ = again.Shutdown(ctx)
}
