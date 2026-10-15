package cli

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testArchive(t *testing.T, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, name := range names {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRenderCLI(t *testing.T) {
	archive := testArchive(t, "封_面.png", "封_面 (2).png")
	var requests []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			io.WriteString(w, `{"ok":true}`)
		case "/api/projects/p_test":
			io.WriteString(w, `{"ok":true,"project":{"id":"p_test"}}`)
		default:
			requests = append(requests, r)
			if strings.HasSuffix(r.URL.Path, ".zip") {
				w.Write(archive)
			} else {
				w.Write([]byte("image"))
			}
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	execute := func(args ...string) error {
		root := newRootCmd()
		root.SetArgs(append([]string{"--server", srv.URL, "-p", "p_test"}, args...))
		return root.Execute()
	}
	out := filepath.Join(dir, "single.png")
	if err := execute("render", "-o", out); err != nil {
		t.Fatal(err)
	}
	if q := requests[0].URL.Query(); q.Get("board") != "" || q.Get("scale") != "1" || q.Get("quality") != "0.92" || requests[0].URL.Path != "/api/projects/p_test/export.png" {
		t.Fatal(requests[0].URL)
	}
	if err := execute("render", "-b", "2", "--format", "webp", "--scale", "3", "--quality", "0.7", "--transparent", "--ids", "标题,副标题", "--ids", "人像", "-o", out); err != nil {
		t.Fatal(err)
	}
	q := requests[1].URL.Query()
	if q.Get("board") != "2" || q.Get("scale") != "3" || q.Get("quality") != "0.7" || q.Get("transparent") != "true" || !reflect.DeepEqual(q["ids"], []string{"标题", "副标题", "人像"}) {
		t.Fatal(q)
	}
	archivePath := filepath.Join(dir, "all.ZIP")
	if err := execute("render", "--all-boards", "--format", "jpg", "--scale", "2", "-o", archivePath); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(archivePath)
	if err != nil || !bytes.Equal(got, archive) {
		t.Fatal(err)
	}
	if requests[2].URL.Query().Get("format") != "jpg" || requests[2].URL.Path != "/api/projects/p_test/export.zip" {
		t.Fatal(requests[2].URL)
	}
	folder := filepath.Join(dir, "boards")
	if err := execute("render", "--all-boards", "-o", folder); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(folder)
	if err != nil || len(entries) != 2 {
		t.Fatal(entries, err)
	}
	for _, name := range []string{"封_面.png", "封_面 (2).png"} {
		got, err := os.ReadFile(filepath.Join(folder, name))
		if err != nil || string(got) != name {
			t.Fatal(name, err)
		}
	}
	for _, args := range [][]string{
		{"--all-boards", "-b", "1"}, {"--all-boards", "--ids", "x"}, {"--all-boards", "--clipboard"},
		{"--format", "gif"}, {"--scale", "NaN"}, {"--scale", "0"}, {"--scale", "9"}, {"--quality", "NaN"}, {"--quality", "2"}, {"--quality", "0"}, {"--ids", ""}, {"-o", "x.zip"},
	} {
		if err := execute(append([]string{"render"}, args...)...); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if len(requests) != 4 {
		t.Fatal("invalid flags made network export requests")
	}
}

func TestExtractBoardArchiveGuards(t *testing.T) {
	for _, name := range []string{"../escape.png", "/absolute.png", `..\escape.png`, "C:escape.png"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "archive.zip")
			os.WriteFile(src, testArchive(t, name), 0o644)
			if _, err := extractBoardArchive(src, filepath.Join(dir, "out")); err == nil {
				t.Fatal("unsafe entry accepted")
			}
		})
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "archive.zip")
	os.WriteFile(src, testArchive(t, "cover.png"), 0o644)
	out := filepath.Join(dir, "out")
	os.Mkdir(out, 0o755)
	target := filepath.Join(dir, "original")
	os.WriteFile(target, []byte("keep"), 0o644)
	if err := os.Symlink(target, filepath.Join(out, "cover.png")); err != nil {
		t.Skip(err)
	}
	if _, err := extractBoardArchive(src, out); err == nil {
		t.Fatal("followed symlink")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "keep" {
		t.Fatal("modified unrelated file")
	}
}

func TestRenderDefaultsAndFailedDownload(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	fail := false
	archive := testArchive(t, "only.png")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			io.WriteString(w, `{"ok":true}`)
		case "/api/projects/p_test":
			io.WriteString(w, `{"ok":true,"project":{"id":"p_test"}}`)
		default:
			if fail {
				http.Error(w, `{"error":"render failed"}`, 500)
				return
			}
			if strings.HasSuffix(r.URL.Path, ".zip") {
				w.Write(archive)
			} else {
				w.Write([]byte("old image"))
			}
		}
	}))
	defer srv.Close()
	execute := func(args ...string) error {
		root := newRootCmd()
		root.SetArgs(append([]string{"--server", srv.URL, "-p", "p_test", "render"}, args...))
		return root.Execute()
	}
	if err := execute(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile("davinci-p_test.png"); err != nil || string(got) != "old image" {
		t.Fatal(err, string(got))
	}
	if err := execute("--all-boards"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("davinci-p_test/only.png"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("davinci-p_test/keep.txt", []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := execute("--all-boards"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile("davinci-p_test/keep.txt"); err != nil || string(got) != "keep" {
		t.Fatal(err)
	}
	fail = true
	if err := execute(); err == nil {
		t.Fatal("render failure succeeded")
	}
	if got, _ := os.ReadFile("davinci-p_test.png"); string(got) != "old image" {
		t.Fatal("failed request truncated old export")
	}
}
