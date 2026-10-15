package doc

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestExportNamesContract(t *testing.T) {
	data, err := os.ReadFile("../../testdata/export-names.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Names, Stems []string }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	boards := make([]*Board, len(fixture.Names))
	for i, name := range fixture.Names {
		boards[i] = &Board{Name: name}
	}
	for _, format := range []string{"png", "jpg", "jpeg", "webp"} {
		ext := format
		if ext == "jpeg" {
			ext = "jpg"
		}
		want := make([]string, len(boards))
		for i, stem := range fixture.Stems {
			want[i] = stem + "." + ext
		}
		if got := exportBoardNames(boards, format); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %#v; want %#v", format, got, want)
		}
	}
}

type exportFake struct {
	fake
	boards  []string
	options []RenderOptions
	failAt  int
}

func (f *exportFake) Render(b *Board, opts RenderOptions) ([]byte, error) {
	f.boards = append(f.boards, b.ID)
	f.options = append(f.options, opts)
	if len(f.boards) == f.failAt {
		return nil, errors.New("render failed")
	}
	var out bytes.Buffer
	err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, int(b.Canvas.Width*opts.Scale), int(b.Canvas.Height*opts.Scale))))
	return out.Bytes(), err
}

func exportEngine() (*Engine, *exportFake) {
	p := Blank(20, 10)
	p.Boards[0].ID = "b1"
	p.Boards[0].Name = "封/面"
	p.Boards = append(p.Boards, &Board{ID: "b2", Name: "封:面", Canvas: Canvas{Width: 30, Height: 15}, Layers: []*Layer{}})
	p.Active = "b2"
	f := &exportFake{}
	return NewEngine(p, f), f
}

func TestExportAllBoards(t *testing.T) {
	for _, format := range []string{"png", "jpg", "webp"} {
		t.Run(format, func(t *testing.T) {
			e, f := exportEngine()
			before := string(e.p.JSON())
			r := run(t, e, `{"type":"export","allBoards":true,"format":"`+format+`","multiplier":3,"quality":0.7,"transparent":true}`)
			data := r.Data.(map[string]any)
			raw, err := base64.StdEncoding.DecodeString(strings.SplitN(data["dataURL"].(string), ",", 2)[1])
			if err != nil {
				t.Fatal(err)
			}
			zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
			if err != nil {
				t.Fatal(err)
			}
			if len(zr.File) != 2 || data["format"] != "zip" {
				t.Fatal(data)
			}
			for i, zf := range zr.File {
				want := []string{"封_面.", "封_面 (2)."}[i] + format
				if zf.Name != want {
					t.Fatalf("got %s want %s", zf.Name, want)
				}
				in, err := zf.Open()
				if err != nil {
					t.Fatal(err)
				}
				imageBytes, err := io.ReadAll(in)
				in.Close()
				if err != nil {
					t.Fatal(err)
				}
				cfg, _, err := image.DecodeConfig(bytes.NewReader(imageBytes))
				if err != nil {
					t.Fatal(err)
				}
				if cfg.Width != []int{60, 90}[i] || cfg.Height != []int{30, 45}[i] {
					t.Fatal(cfg)
				}
				opts := f.options[i]
				expectedFormat := format
				if format == "jpg" {
					expectedFormat = "jpeg"
				}
				if opts.Format != expectedFormat || opts.Scale != 3 || opts.Quality != 0.7 || !opts.Transparent || len(opts.IDs) != 0 {
					t.Fatal(opts)
				}
			}
			if !reflect.DeepEqual(f.boards, []string{"b1", "b2"}) || string(e.p.JSON()) != before || r.Changed {
				t.Fatal("export changed project or board order")
			}
		})
	}
}

func TestExportDefaultsSelectionAndFailure(t *testing.T) {
	e, f := exportEngine()
	run(t, e, `{"type":"export"}`)
	if f.boards[0] != "b2" || f.options[0].Scale != 1 || f.options[0].Format != "png" {
		t.Fatal(f)
	}
	run(t, e, `{"type":"export","board":1}`)
	if f.boards[1] != "b1" {
		t.Fatal(f.boards)
	}
	run(t, e, `{"type":"addShape","name":"选择","width":8,"height":6}`)
	run(t, e, `{"type":"export","ids":["选择"],"transparent":true}`)
	if len(f.options[2].IDs) != 1 || f.options[2].IDs[0] != e.p.ActiveBoard().Layers[0].ID {
		t.Fatal(f.options)
	}
	fail(t, e, `{"type":"export","ids":["不存在"]}`, "no layer")
	fail(t, e, `{"type":"export","allBoards":true,"ids":["选择"]}`, "cannot be combined")
	fail(t, e, `{"type":"export","allBoards":true,"board":2}`, "cannot be combined")
	if e.p.Active != "b1" {
		t.Fatal("conflicting flags changed active board")
	}
	e, f = exportEngine()
	f.failAt = 2
	r := e.Apply([]byte(`{"type":"export","allBoards":true}`))
	if r.OK || r.Data != nil || !strings.Contains(r.Error, "render failed") {
		t.Fatal(r)
	}
}

func TestExportSchema(t *testing.T) {
	for _, c := range Schema()["commands"].([]map[string]any) {
		if c["type"] != "export" {
			continue
		}
		have := map[string]bool{}
		for _, p := range c["params"].([]Param) {
			have[p.Name] = true
		}
		for _, key := range []string{"allBoards", "format", "multiplier", "quality", "transparent", "ids"} {
			if !have[key] {
				t.Fatal(key)
			}
		}
		return
	}
	t.Fatal("export schema missing")
}

func TestExportAllSingleLegacyBoard(t *testing.T) {
	p, err := Parse([]byte(`{"version":1,"canvas":{"width":10,"height":8},"layers":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	f := &exportFake{}
	e := NewEngine(p, f)
	r := run(t, e, `{"type":"export","allBoards":true}`)
	if len(r.Data.(map[string]any)["files"].([]string)) != 1 || len(f.boards) != 1 {
		t.Fatal(r)
	}
}
