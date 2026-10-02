package doc

import (
	"fmt"
	"strings"
)

// Param describes one command parameter, for `davinci schema` and for the
// unknown-parameter check.
type Param struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"` // string | number | boolean | object | array
	Desc     string   `json:"desc"`
	Required bool     `json:"required,omitempty"`
	Default  any      `json:"default,omitempty"`
	Enum     []string `json:"enum,omitempty"`
}

// Spec is one command.
type Spec struct {
	Type     string
	Summary  string
	Params   []Param
	Returns  string
	ReadOnly bool
	// OwnHistory marks undo/redo: they walk the history themselves, and an
	// open step around them would clear the very redo stack they read.
	OwnHistory bool
	Mutates    bool
	Run        func(c *Ctx, cmd map[string]any) (any, error)
}

var (
	registry = map[string]*Spec{}
	ordered  []*Spec
)

func define(s Spec) {
	s.Mutates = !s.ReadOnly && !s.OwnHistory
	sp := &s
	registry[s.Type] = sp
	ordered = append(ordered, sp)
}

// REF is the layer reference every layer command takes.
var REF = Param{Name: "id", Type: "string", Desc: "图层 id 或图层名（名字在文档内唯一）", Required: true}

// Ctx is what a command runs against: the project and its active board.
type Ctx struct {
	e *Engine
	P *Project
	B *Board
}

func (c *Ctx) svc() Services { return c.e.svc }

// measure brings text heights up to date now (a command that goes on to use a
// text layer's height, like aligning it, needs the new one).
func (c *Ctx) measure() error { return c.e.measure() }

// layer looks a layer up by id or name and fails with the list of layers.
func (c *Ctx) layer(ref any) (*Layer, error) {
	key := strings.TrimSpace(str(ref))
	if key == "" {
		return nil, errf("a layer id or name is required — layers: %s", c.layerList())
	}
	if l := c.B.Find(key); l != nil {
		return l, nil
	}
	return nil, errf(`no layer "%s" — layers: %s`, key, c.layerList())
}

func (c *Ctx) layerList() string {
	if len(c.B.Layers) == 0 {
		return "(none)"
	}
	parts := make([]string, len(c.B.Layers))
	for i, l := range c.B.Layers {
		parts[i] = fmt.Sprintf("%d:%s[%s]", i, l.Name, l.ID)
	}
	return strings.Join(parts, ", ")
}

// layers resolves an ids array, refusing unknown or repeated entries.
func (c *Ctx) layers(refs any, min int) ([]*Layer, error) {
	list, _ := refs.([]any)
	if len(list) < min {
		s := ""
		if min > 1 {
			s = "s"
		}
		return nil, errf(`ids needs at least %d layer%s, e.g. ["标题", "副标题"]`, min, s)
	}
	seen := map[string]bool{}
	out := make([]*Layer, 0, len(list))
	for _, r := range list {
		l, err := c.layer(r)
		if err != nil {
			return nil, err
		}
		if seen[l.ID] {
			return nil, errf(`layer "%s" appears twice in ids`, l.Name)
		}
		seen[l.ID] = true
		out = append(out, l)
	}
	return out, nil
}

// remove takes a layer off the board.
func (c *Ctx) remove(l *Layer) {
	i := c.B.Index(l.ID)
	if i >= 0 {
		c.B.Layers = append(c.B.Layers[:i], c.B.Layers[i+1:]...)
	}
}

// insert puts a layer at a stack index (clamped).
func (c *Ctx) insert(l *Layer, at int) {
	if at < 0 {
		at = 0
	}
	if at > len(c.B.Layers) {
		at = len(c.B.Layers)
	}
	c.B.Layers = append(c.B.Layers, nil)
	copy(c.B.Layers[at+1:], c.B.Layers[at:])
	c.B.Layers[at] = l
}

// LayerRow is a layer as listings report it.
type LayerRow struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
	Rotation float64 `json:"rotation"`
	Opacity  float64 `json:"opacity"`
	Visible  bool    `json:"visible"`
	Locked   bool    `json:"locked"`
	Preview  string  `json:"preview"`
	// Index is the stacking index setZIndex takes: 0 is the backmost layer.
	Index int `json:"index"`
}

// Rows lists a board's layers front to back.
func Rows(b *Board) []LayerRow {
	out := make([]LayerRow, 0, len(b.Layers))
	for i := len(b.Layers) - 1; i >= 0; i-- {
		l := b.Layers[i]
		out = append(out, LayerRow{
			ID: l.ID, Name: l.Name, Type: l.Type,
			X: round(l.X), Y: round(l.Y), Width: round(l.Width), Height: round(l.Height),
			Rotation: l.Rotation, Opacity: l.Opacity, Visible: l.Visible, Locked: l.Locked,
			Preview: Preview(l), Index: i,
		})
	}
	return out
}

// Preview is a short human summary of a layer.
func Preview(l *Layer) string {
	switch l.Type {
	case "text":
		t := strings.Join(strings.Fields(l.TextOf()), " ")
		r := []rune(t)
		if len(r) > 40 {
			r = r[:40]
		}
		return string(r)
	case "image":
		if l.Image == nil {
			return ""
		}
		u := l.Image.URL
		if r := []rune(u); len(r) > 48 {
			return "…" + string(r[len(r)-47:])
		}
		return u
	case "group":
		return fmt.Sprintf("%d 个图层", len(l.Children))
	}
	kind := "rect"
	if l.Shape != nil {
		kind = l.Shape.Kind
	}
	fill := str(l.S("fill"))
	if fill == "" {
		fill = "-"
	}
	return kind + " " + fill
}

// position is what transform commands answer with.
func position(l *Layer) map[string]any {
	return map[string]any{"x": round(l.X), "y": round(l.Y), "width": round(l.Width), "height": round(l.Height), "rotation": l.Rotation}
}

// Schema describes every command, for `davinci schema` and for AI tools.
func Schema() map[string]any {
	cmds := make([]map[string]any, 0, len(ordered))
	for _, s := range ordered {
		params := s.Params
		if params == nil {
			params = []Param{}
		}
		m := map[string]any{"type": s.Type, "summary": s.Summary, "params": params, "mutates": s.Mutates || s.OwnHistory}
		if s.Returns != "" {
			m["returns"] = s.Returns
		}
		cmds = append(cmds, m)
	}
	return map[string]any{
		"version":       2,
		"generatedBy":   "davinci server",
		"units":         "canvas pixels; layer x/y is its top-left corner; rotation is clockwise degrees",
		"layerRef":      "every command addresses a layer by its id or by its unique name",
		"boardRef":      `a project has one or more boards (画板); commands act on the active board. Add a top-level "board" (board id, name, or 1-based number) to any command — or to a batch — to run it on that board`,
		"commands":      cmds,
		"canvasPresets": CanvasPresets,
		"textPresets":   TextPresets,
		"examples": []map[string]any{
			{"note": "一张小红书封面：背景图 + 主标题 + 副标题", "command": []any{
				map[string]any{"type": "setCanvasSize", "preset": "xhs-3-4"},
				map[string]any{"type": "addImage", "path": "~/Pictures/bg.jpg", "x": 0, "y": 0, "width": 1242, "height": 1656},
				map[string]any{"type": "addText", "text": "AI 编程实测", "x": 96, "y": 420, "width": 1050, "style": map[string]any{"fontSize": 150, "fill": "#ffffff", "stroke": "#000000:8", "paintFirst": true}},
				map[string]any{"type": "addText", "text": "10 个工具，一周省 20 小时", "x": 96, "y": 640, "width": 1050, "style": map[string]any{"fontSize": 56, "fill": "rgba(255,255,255,.9)"}},
			}},
			{"note": "把某个图层放到最上层", "command": map[string]any{"type": "bringToFront", "id": "标题"}},
			{"note": "导出 2 倍图", "command": map[string]any{"type": "export", "format": "png", "multiplier": 2}},
		},
	}
}

// Mutates reports whether a command may change the document.
func Mutates(raw []byte) bool {
	cmd, err := decode(raw)
	if err != nil {
		return true
	}
	if str(cmd["type"]) == "batch" {
		return true
	}
	s := registry[str(cmd["type"])]
	return s == nil || s.Mutates || s.OwnHistory
}
