package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestProjectMetadataCLI(t *testing.T) {
	saved := gf
	t.Cleanup(func() { gf = saved })
	for _, tc := range []struct {
		name         string
		args         []string
		method, path string
		want         map[string]any
	}{
		{"add", []string{"tpl", "add", "https://example.com/image.png", "--source", "小红书", "--author", "作者", "--likes", "123", "--link", "https://example.com/post", "--note", "备注", "--tags", "大字"}, "POST", "/api/projects/from-image", map[string]any{"ref": "https://example.com/image.png", "source": "小红书", "author": "作者", "likes": "123", "link": "https://example.com/post", "note": "备注", "tags": "大字", "kind": "template"}},
		{"update", []string{"tpl", "update", "p_template", "--source", "B站", "--author", "作者", "--likes", "321"}, "PATCH", "/api/projects/p_template", map[string]any{"source": "B站", "author": "作者", "likes": float64(321)}},
		{"clear", []string{"tpl", "update", "p_template", "--source", "", "--author", "", "--likes", "0", "--note", "", "--link", ""}, "PATCH", "/api/projects/p_template", map[string]any{"source": "", "author": "", "likes": float64(0), "note": "", "link": ""}},
		{"project-template", []string{"project", "update", "p_design", "--template-id", "p_template"}, "PATCH", "/api/projects/p_design", map[string]any{"templateId": "p_template"}},
		{"clear-template", []string{"projects", "update", "p_design", "--template-id", ""}, "PATCH", "/api/projects/p_design", map[string]any{"templateId": ""}},
		{"use", []string{"tpl", "use", "p_template", "新作品"}, "POST", "/api/projects/p_template/duplicate", map[string]any{"kind": "design", "name": "新作品"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == tc.method && r.URL.Path == tc.path {
					got = map[string]any{}
					if tc.name == "add" {
						if err := r.ParseMultipartForm(1 << 20); err != nil {
							t.Error(err)
						}
						for k, values := range r.MultipartForm.Value {
							got[k] = values[0]
						}
					} else if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
						t.Error(err)
					}
					fmt.Fprint(w, `{"ok":true,"project":{"id":"p_design","templateId":"p_template"}}`)
					return
				}
				if r.URL.Path == "/api/health" {
					fmt.Fprint(w, `{"ok":true}`)
					return
				}
				fmt.Fprintf(w, `{"ok":true,"project":{"id":%q}}`, r.URL.Path[len("/api/projects/"):])
			}))
			defer srv.Close()
			root := newRootCmd()
			root.SetArgs(append([]string{"--server", srv.URL}, tc.args...))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("request = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestMetadataLikesFlagsRejectInvalidValues(t *testing.T) {
	saved := gf
	t.Cleanup(func() { gf = saved })
	for _, args := range [][]string{
		{"tpl", "add", "missing.png", "--likes", "-1"},
		{"tpl", "add", "missing.png", "--likes", "1.5"},
		{"tpl", "update", "p_template", "--likes", "1.5"},
		{"tpl", "update", "p_template", "--likes", "-1"},
	} {
		root := newRootCmd()
		root.SetArgs(args)
		if err := root.Execute(); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
