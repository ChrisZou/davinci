package doc

import (
	"encoding/json"
	"reflect"
	"testing"
)

const testGradient = `{"type":"linear","angle":-90,"stops":[{"color":"#ff0000"},{"color":"#0000ff","opacity":0}]}`

func TestGradientCommandsPersistenceAndHistory(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addShape","kind":"rect","x":10,"y":20,"width":200,"height":100,"name":"rect","fill":"#00ff00","fillGradient":`+testGradient+`}`)
	g := layer(e, "rect").S("fillGradient").(map[string]any)
	if g["angle"] != 270.0 || g["stops"].([]any)[0].(map[string]any)["opacity"] != 1.0 {
		t.Fatalf("gradient defaults/normalization: %v", g)
	}
	parsed, err := Parse(e.Snapshot())
	if err != nil || !reflect.DeepEqual(parsed.ActiveBoard().Find("rect").S("fillGradient"), g) {
		t.Fatalf("gradient did not survive JSON save/load: %v", err)
	}
	run(t, e, `{"type":"duplicateLayer","id":"rect"}`)
	if !reflect.DeepEqual(e.p.ActiveBoard().Layers[1].S("fillGradient"), g) {
		t.Fatal("duplicate lost gradient")
	}
	run(t, e, `{"type":"setShapeProps","id":"rect","fillGradient":null}`)
	if layer(e, "rect").S("fillGradient") != nil || layer(e, "rect").S("fill") != "#00ff00" {
		t.Fatal("solid toggle did not restore the original colour")
	}
	run(t, e, `{"type":"undo"}`)
	if !reflect.DeepEqual(layer(e, "rect").S("fillGradient"), g) {
		t.Fatal("undo lost gradient")
	}
	run(t, e, `{"type":"redo"}`)
	if layer(e, "rect").S("fillGradient") != nil {
		t.Fatal("redo did not restore solid fill")
	}
	for _, command := range []string{
		`{"type":"setShapeProps","id":"rect","fillGradient":` + testGradient + `}`,
		`{"type":"updateLayer","id":"rect","props":{"fill":"#ffffff","fillGradient":` + testGradient + `}}`,
	} {
		run(t, e, command)
		if !reflect.DeepEqual(layer(e, "rect").S("fillGradient"), g) {
			t.Fatal("command lost gradient")
		}
		run(t, e, `{"type":"updateLayer","id":"rect","props":{"color":"#123456"}}`)
		if layer(e, "rect").S("fillGradient") != nil {
			t.Fatal("solid colour edit left an overriding gradient")
		}
	}
	// Existing solid-only edits remain visible even after a gradient was used.
	run(t, e, `{"type":"setShapeProps","id":"rect","fillGradient":`+testGradient+`}`)
	run(t, e, `{"type":"setShapeProps","id":"rect","fill":"#ffffff"}`)
	if layer(e, "rect").S("fillGradient") != nil {
		t.Fatal("setShapeProps fill did not clear gradient")
	}
}

func TestMalformedGradientIsAtomic(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addShape","kind":"rect","name":"r","x":0,"y":0,"width":100,"height":60}`)
	for _, invalid := range []string{
		`"linear"`, `{}`, `{"type":"radial","stops":[]}`,
		`{"type":"linear","angle":"90","stops":[]}`,
		`{"type":"linear","stops":[{"color":"#fff"}]}`,
		`{"type":"linear","stops":[{"color":""},{"color":"#fff"}]}`,
		`{"type":"linear","stops":[{"color":"#000","opacity":2},{"color":"#fff"}]}`,
		`{"type":"linear","stops":[{"color":"#000","opacity":null},{"color":"#fff"}]}`,
		`{"type":"linear","stops":[{"color":"#000","offset":0},{"color":"#fff"}]}`,
	} {
		before := string(e.Snapshot())
		r := e.Apply([]byte(`{"type":"updateLayer","id":"r","props":{"x":99,"fillGradient":` + invalid + `}}`))
		if r.OK || string(e.Snapshot()) != before {
			t.Fatalf("invalid gradient changed document: %s → %+v", invalid, r)
		}
	}
	for _, kind := range []string{"text", "image", "line"} {
		l := layer(e, "r")
		l.Type = kind
		if kind == "line" {
			l.Type, l.Shape.Kind = "shape", "line"
		}
		var g any
		_ = json.Unmarshal([]byte(testGradient), &g)
		if err := setFillGradient(l, g); err == nil {
			t.Fatalf("accepted gradient on %s", kind)
		}
	}
}
