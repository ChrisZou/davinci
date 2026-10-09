package doc

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// fake stands in for the renderer: heights follow the layout rule with
// explicit lines only, every character is an em wide, every image is 400×300.
type fake struct{ measured int }

func (f *fake) Measure(ls []*Layer) (map[string]TextSize, error) {
	out := map[string]TextSize{}
	for _, l := range ls {
		f.measured++
		longest := 0
		for _, line := range strings.Split(l.TextOf(), "\n") {
			longest = max(longest, len([]rune(line)))
		}
		w := math.Min(float64(longest)*num(l.S("fontSize"), 40), l.Width/num(l.S("stretch"), 1))
		out[l.ID] = TextSize{Height: estimateHeight(l), Width: w, Lines: strings.Count(l.TextOf(), "\n") + 1}
	}
	return out, nil
}
func (f *fake) Asset(ref string) (string, int, int, error) {
	if strings.Contains(ref, "wide") {
		return "/assets/wide.png", 800, 200, nil
	}
	return "/assets/x.png", 400, 300, nil
}
func (f *fake) Render(*Board, RenderOptions) ([]byte, error) { return nil, nil }
func (f *fake) Fonts() []string                              { return []string{"PingFang SC"} }
func (f *fake) RemoveBackground(ref, model string) (string, int, int, error) {
	return "/assets/cut-" + model + ".png", 400, 300, nil
}

func engine(t *testing.T) (*Engine, *fake) {
	t.Helper()
	f := &fake{}
	return NewEngine(Blank(1242, 1656), f), f
}

func run(t *testing.T, e *Engine, cmd string) Result {
	t.Helper()
	r := e.Apply([]byte(cmd))
	if !r.OK {
		t.Fatalf("%s → %s", cmd, r.Error)
	}
	return r
}

func fail(t *testing.T, e *Engine, cmd, want string) {
	t.Helper()
	r := e.Apply([]byte(cmd))
	if r.OK || !strings.Contains(r.Error, want) {
		t.Fatalf("%s: want error containing %q, got ok=%v %q", cmd, want, r.OK, r.Error)
	}
}

func layer(e *Engine, name string) *Layer { return e.p.ActiveBoard().Find(name) }

func TestAddTextDefaultsAndHeight(t *testing.T) {
	e, _ := engine(t)
	r := run(t, e, `{"type":"addText","text":"主标题\n第二行","name":"标题","style":{"fontSize":100}}`)
	l := layer(e, "标题")
	if l == nil || l.Width != round(1242*0.8) || str(l.S("fill")) != "#111111" || num(l.S("fontSize"), 0) != 100 {
		t.Fatalf("layer = %+v", l)
	}
	want := 100*1.13*1.25 + 100*1.13
	if math.Abs(l.Height-want) > 0.2 {
		t.Fatalf("height = %v, want %v", l.Height, want)
	}
	if r.Data.(map[string]any)["height"] != round(want) {
		t.Fatalf("returned height = %v", r.Data)
	}
}

func TestBatchIsAllOrNothingAndOneUndoStep(t *testing.T) {
	e, _ := engine(t)
	before := string(e.p.JSON())
	fail(t, e, `[{"type":"addShape","kind":"rect","x":0,"y":0,"width":10,"height":10},{"type":"moveLayer","id":"nope","x":1,"y":1}]`, `no layer "nope"`)
	if string(e.p.JSON()) != before {
		t.Fatal("failed batch left changes behind")
	}
	run(t, e, `[{"type":"addShape","kind":"rect","x":0,"y":0,"width":10,"height":10,"name":"a"},{"type":"moveLayer","id":"a","x":5,"y":6}]`)
	if l := layer(e, "a"); l == nil || l.X != 5 || l.Y != 6 {
		t.Fatalf("batch result: %+v", l)
	}
	run(t, e, `{"type":"undo"}`)
	if len(e.p.ActiveBoard().Layers) != 0 {
		t.Fatal("undo should take the whole batch back")
	}
	run(t, e, `{"type":"redo"}`)
	if l := layer(e, "a"); l == nil || l.X != 5 {
		t.Fatal("redo should bring it back")
	}
}

func TestUnknownParameterAndCommand(t *testing.T) {
	e, _ := engine(t)
	fail(t, e, `{"type":"export","scale":2}`, `unknown parameter "scale"`)
	fail(t, e, `{"type":"fly"}`, `unknown command "fly"`)
	run(t, e, `{"type":"addText","text":"x","name":"t"}`)
	fail(t, e, `{"type":"updateLayer","id":"t","props":{"backgroundColor":"#fff"}}`, `unknown style key "backgroundColor"`)
}

func TestBoardParameterAndBoards(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addBoard","name":"原图","preset":"bili-16-9"}`)
	if e.p.ActiveBoard().Name != "原图" || len(e.p.Boards) != 2 {
		t.Fatal("addBoard should switch to the new board")
	}
	run(t, e, `{"type":"addText","text":"首板","name":"t1","board":1}`)
	if e.p.Boards[0].Find("t1") == nil || e.p.Active != e.p.Boards[0].ID {
		t.Fatal("board:1 should run on the first board and bring it up")
	}
	fail(t, e, `{"type":"listLayers","board":"9"}`, `no board "9"`)
	run(t, e, `{"type":"moveBoard","id":"原图","index":1}`)
	if e.p.Boards[0].Name != "原图" {
		t.Fatal("moveBoard")
	}
	run(t, e, `{"type":"undo"}`)
	if e.p.Boards[0].Name != "画板 1" {
		t.Fatal("undo moveBoard")
	}
}

func TestRotateKeepsCentre(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addShape","kind":"rect","x":100,"y":100,"width":200,"height":100,"name":"r"}`)
	l := layer(e, "r")
	cx, cy := Centre(l, FrameOf(l))
	run(t, e, `{"type":"rotateLayer","id":"r","rotation":90}`)
	nx, ny := Centre(l, FrameOf(l))
	if math.Abs(nx-cx) > 1 || math.Abs(ny-cy) > 1 || l.Rotation != 90 {
		t.Fatalf("centre moved: %v,%v → %v,%v", cx, cy, nx, ny)
	}
}

func TestShearKeepsCentreAndGrowsBox(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addText","text":"斜向上","x":100,"y":100,"width":600,"name":"t"}`)
	l := layer(e, "t")
	f := FrameOf(l)
	w0, h0 := f.BoxSize()
	cx, cy := Centre(l, f)
	run(t, e, `{"type":"setTextStyle","id":"t","style":{"skewY":8}}`)
	f = FrameOf(l)
	w1, h1 := f.BoxSize()
	nx, ny := Centre(l, f)
	if math.Abs(nx-cx) > 1 || math.Abs(ny-cy) > 1 {
		t.Fatalf("centre moved: %v,%v → %v,%v", cx, cy, nx, ny)
	}
	// The right end rises by the width × tan(8°); the width does not change.
	if math.Abs(w1-w0) > 0.01 || math.Abs(h1-h0-w0*math.Tan(8*math.Pi/180)) > 0.5 {
		t.Fatalf("box %vx%v → %vx%v", w0, h0, w1, h1)
	}
	// The right end is higher than the left: content x → board y goes up.
	m := ContentMatrix(l, f)
	if _, yr := m.apply(100, 0); yr >= ny {
		t.Fatalf("right end at y=%v, centre %v: not rising", yr, ny)
	}
	run(t, e, `{"type":"updateLayer","id":"t","props":{"skewY":90}}`)
	if l.S("skewY") != 45.0 {
		t.Fatalf("skewY not clamped: %v", l.S("skewY"))
	}
	run(t, e, `{"type":"setTextStyle","id":"t","style":{"skewY":0}}`)
	if l.S("skewY") != nil {
		t.Fatalf("skewY 0 should be dropped: %v", l.S("skewY"))
	}
}

func TestFitTextShrinksTheBoxAndKeepsTheText(t *testing.T) {
	for _, tc := range []struct{ name, style, after, align string }{
		{"left", `{"fontSize":100}`, ``, ""},
		{"rotated, centred", `{"fontSize":100}`, `{"type":"updateLayer","id":"t","props":{"rotation":350}}`, "center"},
		{"sheared with a stroke", `{"fontSize":100,"skewY":4.6,"stroke":"#000000:12","paintFirst":true}`, ``, "center"},
		{"stretched, right", `{"fontSize":100,"stretch":0.8,"textAlign":"right"}`, ``, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := engine(t)
			run(t, e, `{"type":"addText","text":"多简","x":100,"y":300,"width":800,"name":"t","style":`+tc.style+`}`)
			if tc.after != "" {
				run(t, e, tc.after)
			}
			l := layer(e, "t")
			const tw = 200 // two characters of 100
			// The board point at the middle of the line.
			middle := func() (float64, float64) {
				f := FrameOf(l)
				dx := map[string]float64{"left": -(f.CW - tw) / 2, "center": 0, "right": (f.CW - tw) / 2}
				return ContentMatrix(l, f).apply(dx[pick(l.S("textAlign"), []string{"left", "center", "right"}, "left")], 0)
			}
			x0, y0 := middle()
			h0 := Bounds(l, FrameOf(l)).Height
			cmd := `{"type":"fitText","id":"t"}`
			if tc.align != "" {
				cmd = `{"type":"fitText","id":"t","align":"` + tc.align + `"}`
			}
			run(t, e, cmd)
			x1, y1 := middle()
			if math.Abs(x1-x0) > 0.1 || math.Abs(y1-y0) > 0.1 {
				t.Fatalf("text moved: %v,%v → %v,%v", x0, y0, x1, y1)
			}
			if want := math.Ceil(tw * num(l.S("stretch"), 1)); l.Width != want {
				t.Fatalf("width %v, want %v", l.Width, want)
			}
			if tc.align != "" && str(l.S("textAlign")) != tc.align {
				t.Fatalf("textAlign %v, want %v", l.S("textAlign"), tc.align)
			}
			// A sheared or turned box was tall because it was wide.
			if h1 := Bounds(l, FrameOf(l)).Height; tc.name != "left" && tc.name != "stretched, right" && h1 >= h0-1 {
				t.Fatalf("box height %v → %v: did not shrink", h0, h1)
			}
		})
	}
	e, _ := engine(t)
	run(t, e, `[{"type":"addText","text":"a","name":"t"},{"type":"addShape","kind":"rect","x":0,"y":0,"width":50,"height":50,"name":"r"}]`)
	for _, bad := range []string{`{"type":"fitText","id":"r"}`, `{"type":"fitText","id":"t","align":"middle"}`} {
		if r := e.Apply([]byte(bad)); r.OK {
			t.Fatalf("%s should fail", bad)
		}
	}
}

func TestGroupUngroupRoundTrip(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `[{"type":"addShape","kind":"rect","x":100,"y":200,"width":50,"height":60,"name":"a"},{"type":"addShape","kind":"ellipse","x":300,"y":250,"width":80,"height":40,"name":"b"},{"type":"addText","text":"标","x":150,"y":400,"name":"c","style":{"fontSize":60}}]`)
	before := map[string][2]float64{}
	for _, n := range []string{"a", "b", "c"} {
		l := layer(e, n)
		before[n] = [2]float64{l.X, l.Y}
	}
	run(t, e, `{"type":"groupLayers","ids":["a","b","c"],"name":"组"}`)
	if len(e.p.ActiveBoard().Layers) != 1 || layer(e, "组").Type != "group" {
		t.Fatal("group")
	}
	run(t, e, `{"type":"moveLayer","id":"组","x":`+fnum(layer(e, "组").X+10)+`,"y":`+fnum(layer(e, "组").Y)+`}`)
	run(t, e, `{"type":"ungroupLayers","id":"组"}`)
	for _, n := range []string{"a", "b", "c"} {
		l := layer(e, n)
		if l == nil || math.Abs(l.X-(before[n][0]+10)) > 1 || math.Abs(l.Y-before[n][1]) > 1 {
			t.Fatalf("%s moved: %+v, before %v", n, l, before[n])
		}
	}
}

func TestReplaceImageKeepsRatioAndCentre(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addImage","url":"/assets/x.png","x":100,"y":100,"width":400,"height":300,"name":"p"}`)
	run(t, e, `{"type":"replaceImage","id":"p","url":"/assets/wide.png"}`)
	l := layer(e, "p")
	if l.Width != 400 || l.Height != 100 || l.X != 100 || l.Y != 200 || l.Image.OriginalWidth != 800 {
		t.Fatalf("replaced: %+v %+v", l, l.Image)
	}
}

func TestCropKeepsPixelsInPlace(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addImage","url":"/assets/x.png","x":0,"y":0,"width":800,"height":600,"name":"p"}`)
	run(t, e, `{"type":"cropImage","id":"p","x":100,"y":50,"width":200,"height":100}`)
	l := layer(e, "p")
	if l.X != 200 || l.Y != 100 || l.Width != 400 || l.Height != 200 || l.Image.Crop == nil {
		t.Fatalf("cropped: %+v", l)
	}
	run(t, e, `{"type":"cropImage","id":"p","reset":true}`)
	if l := layer(e, "p"); l.X != 0 || l.Width != 800 || l.Image.Crop != nil {
		t.Fatalf("reset: %+v", l)
	}
}

func TestTextStrokeAndPresetEncoding(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addText","text":"x","name":"t","style":{"stroke":"#000:8","paintFirst":true,"textAlign":"left"}}`)
	l := layer(e, "t")
	if l.S("stroke") != "#000:8" || l.S("paintFirst") != true || l.S("textAlign") != nil {
		t.Fatalf("style = %v", l.Style)
	}
	run(t, e, `{"type":"applyTextPreset","id":"t","preset":"outline-black"}`)
	if l.S("stroke") != "#000000:8" {
		t.Fatalf("preset stroke = %v", l.S("stroke"))
	}
	run(t, e, `{"type":"updateLayer","id":"t","props":{"warp":"trapezoid"}}`)
	if l.S("warpAmount") != 50.0 {
		t.Fatalf("warp default = %v", l.Style)
	}
	run(t, e, `{"type":"updateLayer","id":"t","props":{"warp":"none"}}`)
	if l.S("warp") != nil || l.S("warpAmount") != nil {
		t.Fatalf("warp none = %v", l.Style)
	}
}

func TestMeasureOnlyWhatChanged(t *testing.T) {
	e, f := engine(t)
	run(t, e, `{"type":"addText","text":"a","name":"a"}`)
	run(t, e, `{"type":"addText","text":"b","name":"b"}`)
	n := f.measured
	run(t, e, `{"type":"moveLayer","id":"a","x":1,"y":1}`)
	if f.measured != n {
		t.Fatal("a move should not re-measure")
	}
	run(t, e, `{"type":"setText","id":"a","text":"a\nb"}`)
	if f.measured != n+1 {
		t.Fatalf("setText should re-measure just that layer (%d → %d)", n, f.measured)
	}
}

func TestParseLegacySinglePage(t *testing.T) {
	p, err := Parse([]byte(`{"version":1,"canvas":{"width":900,"height":383},"layers":[{"id":"s1","name":"r","type":"shape","x":1,"y":2,"width":3,"height":4,"rotation":0,"opacity":1,"visible":true,"locked":false}]}`))
	if err != nil || len(p.Boards) != 1 || p.Boards[0].Canvas.Width != 900 || len(p.Boards[0].Layers) != 1 || p.Active != p.Boards[0].ID {
		t.Fatalf("parse: %v %+v", err, p)
	}
	var back map[string]any
	_ = json.Unmarshal(p.JSON(), &back)
	if back["version"] != 2.0 {
		t.Fatal("stored as v2")
	}
}

func TestReorderAndZ(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `[{"type":"addShape","kind":"rect","x":0,"y":0,"width":1,"height":1,"name":"a"},{"type":"addShape","kind":"rect","x":0,"y":0,"width":1,"height":1,"name":"b"},{"type":"addShape","kind":"rect","x":0,"y":0,"width":1,"height":1,"name":"c"}]`)
	run(t, e, `{"type":"reorderLayers","ids":["c","a"]}`)
	names := []string{}
	for _, l := range e.p.ActiveBoard().Layers {
		names = append(names, l.Name)
	}
	if strings.Join(names, ",") != "c,a,b" {
		t.Fatalf("order = %v", names)
	}
	run(t, e, `{"type":"bringToFront","id":"c"}`)
	if e.p.ActiveBoard().Index(layer(e, "c").ID) != 2 {
		t.Fatal("bringToFront")
	}
}

func TestRemoveBackgroundAndRestore(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addImage","url":"/assets/x.png","x":10,"y":20,"width":400,"height":300,"name":"p"}`)
	run(t, e, `{"type":"removeBackground","id":"p"}`)
	l := layer(e, "p")
	if l.Image.URL != "/assets/cut-birefnet-general.png" || l.Image.Original != "/assets/x.png" || l.X != 10 || l.Width != 400 {
		t.Fatalf("cut: %+v %+v", l, l.Image)
	}
	// A second model cuts from the original, not from the cut-out.
	run(t, e, `{"type":"removeBackground","id":"p","model":"portrait"}`)
	if l := layer(e, "p"); l.Image.URL != "/assets/cut-birefnet-portrait.png" || l.Image.Original != "/assets/x.png" {
		t.Fatalf("recut: %+v", l.Image)
	}
	run(t, e, `{"type":"removeBackground","id":"p","restore":true}`)
	if l := layer(e, "p"); l.Image.URL != "/assets/x.png" || l.Image.Original != "" {
		t.Fatalf("restore: %+v", l.Image)
	}
	fail(t, e, `{"type":"removeBackground","id":"p","restore":true}`, "没有原图")
}

func TestImageShadow(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addImage","url":"/assets/x.png","x":0,"y":0,"width":400,"height":300,"name":"p"}`)
	run(t, e, `{"type":"setImageProps","id":"p","shadow":"rgba(0,0,0,0.35):40:0:16"}`)
	if s := layer(e, "p").S("shadow"); s != "rgba(0,0,0,0.35):40:0:16" {
		t.Fatalf("shadow = %v", s)
	}
	run(t, e, `{"type":"setImageProps","id":"p","shadow":"none"}`)
	if s := layer(e, "p").S("shadow"); s != nil {
		t.Fatalf("shadow none = %v", s)
	}
}

func TestImageStroke(t *testing.T) {
	e, _ := engine(t)
	run(t, e, `{"type":"addImage","url":"/assets/x.png","x":10,"y":20,"width":400,"height":300,"name":"p"}`)
	run(t, e, `{"type":"setImageProps","id":"p","stroke":"#ffffff","strokeWidth":16}`)
	l := layer(e, "p")
	if l.S("stroke") != "#ffffff" || l.S("strokeWidth") != 16.0 {
		t.Fatalf("stroke = %v", l.Style)
	}
	// The outline sits outside the box; the box itself does not move or grow.
	if b := Bounds(l, FrameOf(l)); b.Left != 10 || b.Width != 400 {
		t.Fatalf("bounds = %+v", b)
	}
	run(t, e, `{"type":"setImageProps","id":"p","stroke":"none"}`)
	if l := layer(e, "p"); l.S("stroke") != nil || l.S("strokeWidth") != nil {
		t.Fatalf("stroke none = %v", l.Style)
	}
}
