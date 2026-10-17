package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"davinci/internal/server"
)

func TestPaletteCLIOriginalImageIntegration(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	data := t.TempDir()
	srv, err := server.NewServer(server.Options{Port: port, DataDir: data, Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Shutdown(context.Background()) })
	makeImage := func(name string, c color.NRGBA) (string, []byte) {
		img := image.NewNRGBA(image.Rect(0, 0, 12, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < 12; x++ {
				img.SetNRGBA(x, y, c)
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		return path, buf.Bytes()
	}
	first, original := makeImage("first.png", color.NRGBA{240, 220, 180, 255})
	second, replacement := makeImage("second.png", color.NRGBA{10, 40, 70, 255})
	run := func(jsonOutput bool, args ...string) (server.Palette, string) {
		t.Helper()
		old := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = w
		root := newRootCmd()
		flags := []string{"--server", srv.Addr(), "--data", data}
		if jsonOutput {
			flags = append(flags, "--json")
		}
		root.SetArgs(append(append(flags, "palette"), args...))
		err = root.Execute()
		w.Close()
		os.Stdout = old
		raw, _ := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(args, err, string(raw))
		}
		var p server.Palette
		if jsonOutput && json.Unmarshal(raw, &p) != nil {
			t.Fatal("invalid JSON", string(raw))
		}
		return p, string(raw)
	}
	count := func(want int) {
		t.Helper()
		files, err := os.ReadDir(filepath.Join(data, "assets"))
		if err != nil || len(files) != want {
			t.Fatal("asset count", len(files), want, err)
		}
	}
	run(true, "add", "--from-image", first, "--dry-run", "--bg", "#AABBCC")
	count(0)
	ps, _ := srv.Store().Palettes()
	if len(ps) != 0 {
		t.Fatal("dry-run saved palette")
	}
	p, _ := run(true, "add", "--from-image", first, "--name", "原图测试", "--bg", "#AABBCC")
	if p.ImageURL == "" || p.BG != "#aabbcc" {
		t.Fatal(p)
	}
	count(1)
	checkBytes := func(url string, want []byte) {
		t.Helper()
		resp, err := http.Get(srv.Addr() + url)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !bytes.Equal(got, want) {
			t.Fatal("image bytes", resp.StatusCode)
		}
	}
	checkBytes(p.ImageURL, original)
	_, shown := run(false, "show", p.Name)
	if !strings.Contains(shown, srv.Addr()+p.ImageURL) {
		t.Fatal("show missing URL", shown)
	}
	updated, _ := run(true, "update", p.Name, "--image", second)
	if updated.ImageURL == p.ImageURL || updated.PaletteColors != p.PaletteColors || updated.Name != p.Name {
		t.Fatal(updated)
	}
	checkBytes(updated.ImageURL, replacement)
	count(2)
	shownJSON, _ := run(true, "show", p.ID)
	if shownJSON.ImageURL != updated.ImageURL {
		t.Fatal("show JSON missing URL")
	}
	extracted, _ := server.ExtractPalette(replacement)
	updated, _ = run(true, "update", p.ID, "--image", second, "--reextract", "--accent", "#ABCDEF")
	extracted.Accent = "#abcdef"
	if updated.PaletteColors != extracted || updated.Name != p.Name {
		t.Fatal(updated, extracted)
	}
	count(2)
	plain, _ := run(true, "add", "--from-image", first)
	if plain.ImageURL != p.ImageURL {
		t.Fatal("add without overrides lost original")
	}
	count(2)
}
