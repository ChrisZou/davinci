package doc

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
)

func init() {
	define(Spec{
		Type: "insertLayers", Summary: "插入完整的图层 JSON（粘贴 / 跨项目复制用）；自动分配新 id，重名自动加后缀，放在最上层",
		Params: []Param{
			{Name: "layers", Type: "array", Required: true, Desc: "Layer 对象数组（getLayer / getDocument 的输出格式），从底到顶"},
			{Name: "offset", Type: "number", Desc: "整体向右下偏移的像素", Default: 0},
			{Name: "x", Type: "number", Desc: "整体外框左上角放到这个 x（给了就忽略 offset）"},
			{Name: "y", Type: "number", Desc: "整体外框左上角放到这个 y（给了就忽略 offset）"},
		},
		Returns: "{ ids, names }",
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			list, _ := cmd["layers"].([]any)
			if len(list) == 0 {
				return nil, errf("layers must be a non-empty array of Layer objects")
			}
			layers := make([]*Layer, 0, len(list))
			for i, raw := range list {
				m, ok := raw.(map[string]any)
				if !ok {
					return nil, errf("layers[%d] is not an object", i)
				}
				t := str(m["type"])
				if t != "text" && t != "image" && t != "shape" && t != "group" {
					return nil, errf(`layers[%d]: unknown type "%s"`, i, t)
				}
				b, _ := json.Marshal(m)
				var l Layer
				if err := json.Unmarshal(b, &l); err != nil {
					return nil, errf("layers[%d]: %v", i, err)
				}
				l.Visible = boolean(m["visible"], true)
				if _, ok := m["opacity"]; !ok {
					l.Opacity = 1
				}
				layers = append(layers, freshIDs(&l))
			}
			dx := round(num(cmd["offset"], 0))
			dy := dx
			if has(cmd, "x") || has(cmd, "y") {
				left, top := layers[0].X, layers[0].Y
				for _, l := range layers {
					left, top = math.Min(left, l.X), math.Min(top, l.Y)
				}
				dx, dy = 0, 0
				if has(cmd, "x") {
					dx = round(num(cmd["x"], left)) - left
				}
				if has(cmd, "y") {
					dy = round(num(cmd["y"], top)) - top
				}
			}
			ids := make([]string, 0, len(layers))
			names := make([]string, 0, len(layers))
			for _, l := range layers {
				name := strings.TrimSpace(l.Name)
				if name == "" {
					name = "图层"
				}
				l.Name = c.B.UniqueName(name)
				l.X, l.Y = round(l.X)+dx, round(l.Y)+dy
				l.Opacity = clamp(l.Opacity, 0, 1)
				c.B.Layers = append(c.B.Layers, l)
				ids = append(ids, l.ID)
				names = append(names, l.Name)
			}
			return map[string]any{"ids": ids, "names": names}, nil
		},
	})

	define(Spec{
		Type: "alignLayers", Summary: "多个图层互相对齐（对齐到它们的整体外框），或一起对齐到画布",
		Params: []Param{
			{Name: "ids", Type: "array", Desc: "图层 id 或名字数组", Required: true},
			{Name: "h", Type: "string", Desc: "水平对齐", Enum: []string{"left", "center", "right"}},
			{Name: "v", Type: "string", Desc: "垂直对齐", Enum: []string{"top", "middle", "bottom"}},
			{Name: "to", Type: "string", Desc: "selection 对齐到选中图层的外框 / canvas 对齐到画布", Enum: []string{"selection", "canvas"}, Default: "selection"},
		},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			to := pick(cmd["to"], []string{"selection", "canvas"}, "selection")
			min := 2
			if to == "canvas" {
				min = 1
			}
			ls, err := c.layers(cmd["ids"], min)
			if err != nil {
				return nil, err
			}
			h, v := strings.TrimSpace(str(cmd["h"])), strings.TrimSpace(str(cmd["v"]))
			if h == "" && v == "" {
				return nil, errf("give h and/or v")
			}
			boxes := make([]Rect, len(ls))
			for i, l := range ls {
				boxes[i] = Bounds(l, FrameOf(l))
			}
			frame := Rect{0, 0, c.B.Canvas.Width, c.B.Canvas.Height}
			if to == "selection" {
				frame = Union(boxes)
			}
			moved := make([]any, 0, len(ls))
			for i, l := range ls {
				b := boxes[i]
				var dx, dy float64
				switch h {
				case "left":
					dx = frame.Left - b.Left
				case "center":
					dx = (frame.Left+frame.Right())/2 - (b.Left + b.Width/2)
				case "right":
					dx = frame.Right() - b.Right()
				}
				switch v {
				case "top":
					dy = frame.Top - b.Top
				case "middle":
					dy = (frame.Top+frame.Bottom())/2 - (b.Top + b.Height/2)
				case "bottom":
					dy = frame.Bottom() - b.Bottom()
				}
				l.X, l.Y = round(l.X+dx), round(l.Y+dy)
				p := position(l)
				p["name"] = l.Name
				moved = append(moved, p)
			}
			return map[string]any{"moved": moved}, nil
		},
	})

	define(Spec{
		Type: "distributeLayers", Summary: "等间距分布：首尾两个图层不动，中间的图层让间隙相等（至少 3 个）",
		Params: []Param{
			{Name: "ids", Type: "array", Desc: "图层 id 或名字数组", Required: true},
			{Name: "axis", Type: "string", Desc: "h 水平分布 / v 垂直分布", Enum: []string{"h", "v"}, Required: true},
		},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			ls, err := c.layers(cmd["ids"], 3)
			if err != nil {
				return nil, err
			}
			axis := pick(cmd["axis"], []string{"h", "v"}, "")
			if axis == "" {
				return nil, errf(`axis must be "h" or "v"`)
			}
			type item struct {
				l *Layer
				b Rect
			}
			items := make([]item, len(ls))
			for i, l := range ls {
				items[i] = item{l, Bounds(l, FrameOf(l))}
			}
			start := func(b Rect) float64 {
				if axis == "h" {
					return b.Left
				}
				return b.Top
			}
			size := func(b Rect) float64 {
				if axis == "h" {
					return b.Width
				}
				return b.Height
			}
			sort.SliceStable(items, func(i, j int) bool { return start(items[i].b) < start(items[j].b) })
			first, last := items[0].b, items[len(items)-1].b
			total := 0.0
			for _, it := range items {
				total += size(it.b)
			}
			gap := (start(last) + size(last) - start(first) - total) / float64(len(items)-1)
			at := start(first)
			for _, it := range items {
				d := round(at) - start(it.b)
				if axis == "h" {
					it.l.X = round(it.l.X + d)
				} else {
					it.l.Y = round(it.l.Y + d)
				}
				at += size(it.b) + gap
			}
			return map[string]any{"gap": roundTo(gap, 1)}, nil
		},
	})

	define(Spec{
		Type: "groupLayers", Summary: "把几个图层编成一组（组整体移动/缩放/旋转；要改组内图层先 ungroupLayers）",
		Params: []Param{
			{Name: "ids", Type: "array", Desc: "图层 id 或名字数组（至少 2 个）", Required: true},
			{Name: "name", Type: "string", Desc: `组名，默认"编组"`},
		},
		Returns: "{ id, name }",
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			ls, err := c.layers(cmd["ids"], 2)
			if err != nil {
				return nil, err
			}
			// Members keep their stacking order; the group takes the topmost's place.
			sort.SliceStable(ls, func(i, j int) bool { return c.B.Index(ls[i].ID) < c.B.Index(ls[j].ID) })
			at := c.B.Index(ls[len(ls)-1].ID) - (len(ls) - 1)
			boxes := make([]Rect, len(ls))
			for i, l := range ls {
				boxes[i] = Bounds(l, FrameOf(l))
			}
			box := Union(boxes)
			children := make([]*Layer, len(ls))
			for i, l := range ls {
				ch := l.Clone()
				ch.X, ch.Y = l.X-box.Left, l.Y-box.Top
				children[i] = ch
				c.remove(l)
			}
			name := strings.TrimSpace(str(cmd["name"]))
			if name == "" {
				name = "编组"
			}
			g := &Layer{
				ID: NewID("g"), Name: c.B.UniqueName(name), Type: "group",
				X: box.Left, Y: box.Top, Opacity: 1, Visible: true, Children: children,
			}
			mb := MembersBox(children)
			g.Width, g.Height = mb.Width, mb.Height
			c.insert(g, at)
			return map[string]any{"id": g.ID, "name": g.Name}, nil
		},
	})

	define(Spec{
		Type: "ungroupLayers", Summary: "解散编组：组内图层回到画布上，位置/缩放/旋转保持所见即所得",
		Params:  []Param{REF},
		Returns: "{ ids, names }",
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			g, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			if g.Type != "group" {
				return nil, errf(`layer "%s" is a %s, not a group`, g.Name, g.Type)
			}
			at := c.B.Index(g.ID)
			gf := FrameOf(g)
			mb := MembersBox(g.Children)
			// Group units → board: the group's content transform, shifted so
			// the members' own coordinates (from the box's corner) land right.
			gm := mul(ContentMatrix(g, gf), Mat{1, 0, 0, 1, -gf.CW/2 - mb.Left, -gf.CH/2 - mb.Top})
			c.remove(g)
			ids := []string{}
			names := []string{}
			for i, m := range g.Children {
				f := FrameOf(m)
				lx, ly := Centre(m, f)
				bx, by := gm.apply(lx, ly)
				bakeScale(m, gf.ScaleX, gf.ScaleY)
				m.Rotation = normAngle(g.Rotation + m.Rotation)
				PlaceCentre(m, FrameOf(m), bx, by)
				m.X, m.Y = round(m.X), round(m.Y)
				m.Opacity = clamp(m.Opacity*g.Opacity, 0, 1)
				name := strings.TrimSpace(m.Name)
				if name == "" {
					name = "图层"
				}
				m.Name = c.B.UniqueName(name)
				if m.ID == "" {
					m.ID = NewID(prefixOf(m.Type))
				}
				c.insert(m, at+i)
				ids = append(ids, m.ID)
				names = append(names, m.Name)
			}
			if err := c.measure(); err != nil {
				return nil, err
			}
			return map[string]any{"ids": ids, "names": names}, nil
		},
	})
}

// bakeScale folds a scale into a layer's own size: a text gets a bigger font
// (what the document can store for text), everything else a bigger box.
func bakeScale(l *Layer, sx, sy float64) {
	if math.Abs(sx-1) < 1e-6 && math.Abs(sy-1) < 1e-6 {
		return
	}
	switch l.Type {
	case "text":
		l.SetS("fontSize", math.Max(1, round(num(l.S("fontSize"), 40)*sy)))
		l.Width = math.Max(8, round(l.Width*sx))
		if cw, w, ok := splitStroke(str(l.S("stroke"))); ok && cw != "" {
			l.SetS("stroke", cw+":"+fnum(roundTo(w*sy, 1)))
		}
		if p := num(l.S("padding"), 0); p > 0 {
			l.SetS("padding", round(p*sy))
		}
	case "group":
		l.Width, l.Height = l.Width*sx, l.Height*sy
	default:
		l.Width, l.Height = l.Width*sx, l.Height*sy
	}
}
