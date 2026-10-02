// Package web embeds the built editor frontend so a single `davinci serve`
// binary can host both the API and the UI.
package web

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

//go:embed all:dist
var files embed.FS

// Built reports whether a frontend build is present in the binary.
func Built() bool {
	dir, err := fs.Sub(files, "dist")
	if err != nil {
		return false
	}
	entries, err := fs.ReadDir(dir, ".")
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			return true
		}
	}
	return false
}

// FS returns the built frontend rooted at dist/.
func FS() (fs.FS, error) {
	return fs.Sub(files, "dist")
}

// Handler serves the frontend with a single-page-app fallback: a path that does
// not match a file gets index.html, so /editor/<id> needs no server-side router.
//
// boot is stamped into the HTML, so a page carries the id of the server
// process that served it (the same id /api/health reports).
func Handler(boot string) (http.Handler, error) {
	sub, err := FS()
	if err != nil {
		return nil, err
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil, err
	}
	index = stampBoot(index, boot)
	fileServer := http.FileServer(http.FS(sub))
	serveIndex := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(index)))
		_, _ = w.Write(index)
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, "/")), "/")
		if f, err := sub.Open(upath); err == nil {
			_ = f.Close()
			if ct := mime.TypeByExtension(path.Ext(upath)); ct != "" {
				w.Header().Set("Content-Type", ct)
			}
			if upath == "index.html" {
				serveIndex(w)
				return
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		serveIndex(w)
	})
	return h, nil
}

// stampBoot replaces the boot placeholder in the built HTML. A page built
// without one (an older dist, or a hand-edited index.html) is served untouched,
// which the handshake treats as "no opinion" rather than as stale.
func stampBoot(html []byte, boot string) []byte {
	return bytes.Replace(html, []byte("%%DAVINCI_BOOT%%"), []byte(boot), 1)
}

// RendererFiles returns the server-side renderer (a Node script) and the
// CanvasKit WebAssembly it runs on, as built into web/dist/renderer. The
// editor page loads the same canvaskit.wasm, so what the server exports is
// drawn by the very code the editor draws with.
func RendererFiles() (script, wasm []byte, err error) {
	if script, err = files.ReadFile("dist/renderer/renderer.cjs"); err != nil {
		return nil, nil, err
	}
	if wasm, err = files.ReadFile("dist/renderer/canvaskit.wasm"); err != nil {
		return nil, nil, err
	}
	return script, wasm, nil
}
