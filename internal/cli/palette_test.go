package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPaletteCLI(t *testing.T) {
	draft := map[string]any{"id": "pal_test", "name": "图片", "bg": "#ffffff", "text": "#000000", "highlight": "#ff4400", "text2": "#666666", "accent": "#0077aa"}
	saved := 0
	analyzed := 0
	var command map[string]any
	var patch map[string]string
	var deleted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		respond := func(v any) { json.NewEncoder(w).Encode(v) }
		switch r.URL.Path {
		case "/api/health":
			respond(map[string]bool{"ok": true})
		case "/api/projects/p_test":
			respond(map[string]any{"ok": true, "project": map[string]string{"id": "p_test"}})
		case "/api/palettes/analyze":
			if r.Method != "POST" {
				t.Error(r.Method)
			}
			f, h, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				http.Error(w, "file", 400)
				return
			}
			defer f.Close()
			data, _ := io.ReadAll(f)
			if h.Filename != "sample.png" || string(data) != "image bytes" {
				t.Error("upload", h.Filename, string(data))
			}
			if n := r.FormValue("name"); n != "" {
				draft["name"] = n
			}
			analyzed++
			respond(map[string]any{"palette": draft})
		case "/api/palettes":
			if r.Method == "POST" {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
				}
				defer r.MultipartForm.RemoveAll()
				if err := json.Unmarshal([]byte(r.FormValue("palette")), &draft); err != nil {
					t.Error(err)
				}
				f, _, err := r.FormFile("file")
				if err != nil {
					t.Fatal(err)
				}
				data, _ := io.ReadAll(f)
				f.Close()
				if string(data) != "image bytes" {
					t.Error("missing original image")
				}
				draft["id"] = "pal_test"
				saved++
				respond(map[string]any{"palette": draft})
			} else {
				respond(map[string]any{"palettes": []any{draft}})
			}
		case "/api/palettes/pal_test":
			if r.Method == "PATCH" {
				json.NewDecoder(r.Body).Decode(&patch)
			}
			if r.Method == "DELETE" {
				deleted = true
			}
			respond(map[string]any{"ok": true, "palette": draft})
		case "/api/projects/p_test/commands":
			json.NewDecoder(r.Body).Decode(&command)
			respond(map[string]any{"ok": true, "data": map[string]string{"message": "已应用配色"}})
		default:
			http.Error(w, `{"error":"unexpected route"}`, 404)
		}
	}))
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "sample.png")
	if err := os.WriteFile(file, []byte("image bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	execute := func(args ...string) (string, error) {
		t.Helper()
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		root := newRootCmd()
		root.SetArgs(append([]string{"--server", srv.URL, "--json", "palette"}, args...))
		err := root.Execute()
		w.Close()
		os.Stdout = old
		raw, _ := io.ReadAll(r)
		r.Close()
		if err == nil && !json.Valid(bytes.TrimSpace(raw)) {
			t.Fatalf("not JSON: %s", raw)
		}
		return string(raw), err
	}
	if out, err := execute("add", "--from-image", file, "--name", "预览", "--dry-run"); err != nil || !strings.Contains(out, "预览") || saved != 0 || analyzed != 1 {
		t.Fatal(out, err, saved, analyzed)
	}
	if _, err := execute("add", "--from-image", file, "--bg", "#AABBCC"); err != nil || saved != 1 || draft["bg"] != "#aabbcc" {
		t.Fatal(err, saved, draft)
	}
	if out, err := execute("list", "-q", "不存在"); err != nil || !strings.Contains(out, `"palettes": []`) {
		t.Fatal(out, err)
	}
	for _, args := range [][]string{{"list"}, {"show", "pal_test"}, {"update", "pal_test", "--text", "#112233", "--name", "重命名"}, {"apply", "pal_test", "-p", "p_test", "--board", "2"}, {"delete", "pal_test"}} {
		if _, err := execute(args...); err != nil {
			t.Fatal(args, err)
		}
	}
	if patch["text"] != "#112233" || patch["name"] != "重命名" || len(patch) != 2 {
		t.Fatal(patch)
	}
	if command["type"] != "applyPalette" || command["id"] != "pal_test" || command["board"] != "2" || !deleted {
		t.Fatal(command, deleted)
	}
	for _, args := range [][]string{{"add"}, {"update", "pal_test"}, {"show"}, {"list", "unexpected"}, {"add", "--from-image", file, "--bg", "red", "--dry-run"}, {"add", "--from-image", file + "missing"}} {
		if _, err := execute(args...); err == nil {
			t.Fatal("expected failure", args)
		}
	}
}
func TestPaletteCLIAnalysisFailureDoesNotSave(t *testing.T) {
	saves := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			io.WriteString(w, `{"ok":true}`)
			return
		}
		if r.URL.Path == "/api/palettes" {
			saves++
		}
		w.WriteHeader(400)
		io.WriteString(w, `{"ok":false,"error":"图片没有可见颜色"}`)
	}))
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "empty.png")
	os.WriteFile(file, []byte("image"), 0600)
	root := newRootCmd()
	root.SetArgs([]string{"--server", srv.URL, "palette", "add", "--from-image", file})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "没有可见颜色") || saves != 0 {
		t.Fatal(err, saves)
	}
}

func TestPaletteCLIResolvesIDOrExactName(t *testing.T) {
	palettes := []map[string]string{
		{"id": "pal_sunset", "name": "日落橘"},
		{"id": "pal_a", "name": "重名"},
		{"id": "pal_b", "name": "重名"},
		{"id": "pal_collision", "name": "pal_sunset"},
		{"id": "pal_named", "name": "pal_like"},
		{"id": "pal_english", "name": "Sunset"},
	}
	var target, method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/health":
			io.WriteString(w, `{"ok":true}`)
		case r.URL.Path == "/api/palettes":
			json.NewEncoder(w).Encode(map[string]any{"palettes": palettes})
		case strings.HasPrefix(r.URL.Path, "/api/palettes/"):
			target, method = strings.TrimPrefix(r.URL.Path, "/api/palettes/"), r.Method
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "palette": map[string]string{"id": target}})
		case r.URL.Path == "/api/projects/p_test":
			io.WriteString(w, `{"ok":true,"project":{"id":"p_test"}}`)
		case r.URL.Path == "/api/projects/p_test/commands":
			var command map[string]any
			json.NewDecoder(r.Body).Decode(&command)
			target, _ = command["id"].(string)
			method = r.Method
			if command["type"] != "applyPalette" || command["board"] != "2" {
				t.Error("apply command", command)
			}
			io.WriteString(w, `{"ok":true,"data":{}}`)
		default:
			t.Errorf("unexpected route: %s", r.URL.Path)
			http.Error(w, "unexpected route", 404)
		}
	}))
	defer srv.Close()
	for _, action := range []struct{ name, method string }{{"show", "GET"}, {"update", "PATCH"}, {"delete", "DELETE"}, {"apply", "POST"}} {
		for _, ref := range []struct{ input, id, errorText string }{
			{"pal_sunset", "pal_sunset", ""}, // IDs win even when another palette has this name.
			{"日落橘", "pal_sunset", ""},
			{"pal_like", "pal_named", ""}, // An ID-shaped name still resolves by exact name.
			{"Sunset", "pal_english", ""},
			{"重名", "", "重名"},
			{"日落", "", "找不到配色"},
			{"日落橘 ", "", "找不到配色"},
			{"sunset", "", "找不到配色"},
			{"pal_missing", "", "找不到配色"},
		} {
			t.Run(action.name+"/"+ref.input, func(t *testing.T) {
				target, method = "", ""
				root := newRootCmd()
				args := []string{"--server", srv.URL, "--json", "palette", action.name, ref.input}
				if action.name == "update" {
					args = append(args, "--name", "新版")
				}
				if action.name == "apply" {
					args = append(args, "-p", "p_test", "--board", "2")
				}
				root.SetArgs(args)
				err := root.Execute()
				if ref.errorText != "" {
					if err == nil || !strings.Contains(err.Error(), ref.errorText) || target != "" {
						t.Fatalf("ambiguous/missing name must stop before action: target=%s err=%v", target, err)
					}
					if ref.input == "重名" && (!strings.Contains(err.Error(), "pal_a") || !strings.Contains(err.Error(), "pal_b")) {
						t.Fatal("missing candidate IDs", err)
					}
					return
				}
				if err != nil || target != ref.id || method != action.method {
					t.Fatalf("target=%s method=%s err=%v", target, method, err)
				}
			})
		}
	}
}

func TestPaletteResolutionPropagatesListFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":"配色库读取失败"}`)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.paletteID("日落橘"); err == nil || !strings.Contains(err.Error(), "配色库读取失败") {
		t.Fatal("list failure must not be reported as a missing palette", err)
	}
}
