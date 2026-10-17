package doc

import (
	"bytes"
	"fmt"
	"testing"
)

type paletteFake struct{ fake }

func (*paletteFake) PaletteColors(id string) (PaletteColors, error) {
	if id != "pal_test" {
		return PaletteColors{}, fmt.Errorf("missing palette")
	}
	return PaletteColors{BG: "#eeeedd", Text: "#111122", Highlight: "#ff4400", Text2: "#555566", Accent: "#0077aa"}, nil
}
func TestApplyPaletteOneUndoAndTargetBoard(t *testing.T) {
	p := Blank(100, 100)
	b := &Board{ID: "board_second", Name: "第二张", Canvas: Canvas{Width: 100, Height: 100, Background: "#ffffff", BackgroundImage: "/assets/bg.png"}, Layers: []*Layer{
		{ID: "bg", Type: "shape", Width: 100, Height: 100, Style: map[string]any{"fill": "#fff"}},
		{ID: "title", Name: "标题", Type: "text", Height: 10, Style: map[string]any{"fontSize": 60.0, "fill": "#aaa"}},
		{ID: "group", Type: "group", Children: []*Layer{
			{ID: "body", Type: "text", Height: 10, Style: map[string]any{"fontSize": 20.0, "fill": "#bbb"}},
			{ID: "shape", Type: "shape", Width: 20, Height: 20, Style: map[string]any{"fill": "#ccc", "stroke": "#aaa:3", "strokeWidth": 3.0}},
			{ID: "line", Type: "shape", Shape: &ShapeRef{Kind: "line"}, Style: map[string]any{"stroke": "#ddd", "strokeWidth": 4.0}},
			{ID: "empty", Type: "shape", Style: map[string]any{"fill": "transparent"}},
		}},
		{ID: "image", Type: "image", Image: &ImageRef{URL: "/assets/picture.png"}, Style: map[string]any{"fill": "#abc"}},
	}}
	p.Boards = append(p.Boards, b)
	e := NewEngine(p, &paletteFake{})
	before := e.Snapshot()
	fail(t, e, `{"type":"applyPalette","id":"absent","board":"2"}`, "missing palette")
	fail(t, e, `{"type":"applyPalette","id":"pal_test","board":"absent"}`, "找不到画板")
	if !bytes.Equal(before, e.Snapshot()) || e.CanUndo() {
		t.Fatal("invalid apply mutated document/history")
	}
	run(t, e, `{"type":"applyPalette","id":"pal_test","board":"2"}`)
	b = e.p.ActiveBoard()
	if b.ID != "board_second" || b.Canvas.Background != "#eeeedd" || b.Canvas.BackgroundImage != "/assets/bg.png" {
		t.Fatal(b)
	}
	if b.Layers[0].S("fill") != "#eeeedd" || b.Layers[1].S("fill") != "#111122" {
		t.Fatal("background/title mapping")
	}
	children := b.Layers[2].Children
	if children[0].S("fill") != "#555566" || children[1].S("fill") != "#ff4400" || children[1].S("stroke") != "#0077aa:3" || children[2].S("stroke") != "#ff4400" || children[3].S("fill") != "transparent" {
		t.Fatal("nested mapping")
	}
	if b.Layers[3].S("fill") != "#abc" {
		t.Fatal("image changed")
	}
	after := e.Snapshot()
	run(t, e, `{"type":"undo"}`)
	if !bytes.Equal(before, e.Snapshot()) || e.CanUndo() {
		t.Fatal("one undo must restore every board, active board and style")
	}
	run(t, e, `{"type":"redo"}`)
	if !bytes.Equal(after, e.Snapshot()) {
		t.Fatal("redo differs")
	}
	// Reapplying the same palette must not add a history step.
	run(t, e, `{"type":"applyPalette","id":"pal_test"}`)
	run(t, e, `{"type":"undo"}`)
	if !bytes.Equal(before, e.Snapshot()) {
		t.Fatal("idempotent apply added history")
	}
}
func TestApplyPaletteBatchRollback(t *testing.T) {
	e := NewEngine(Blank(100, 100), &paletteFake{})
	before := e.Snapshot()
	fail(t, e, `[{"type":"applyPalette","id":"pal_test"},{"type":"removeLayer","id":"absent"}]`, "no layer")
	if !bytes.Equal(before, e.Snapshot()) || e.CanUndo() {
		t.Fatal("failed batch left palette edits")
	}
}
