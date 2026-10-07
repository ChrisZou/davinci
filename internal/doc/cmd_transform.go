package doc

import (
	"math"
	"strings"
)

func normAngle(a float64) float64 {
	return math.Mod(math.Mod(round(a), 360)+360, 360)
}

func init() {
	define(Spec{
		Type: "moveLayer", Summary: "移动图层到绝对坐标（x/y 是图层左上角）",
		Params: []Param{REF, {Name: "x", Type: "number", Desc: "左上角 x", Required: true}, {Name: "y", Type: "number", Desc: "左上角 y", Required: true}},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			l.X, l.Y = round(num(cmd["x"], l.X)), round(num(cmd["y"], l.Y))
			return position(l), nil
		},
	})

	define(Spec{
		Type: "resizeLayer", Summary: "改变图层尺寸；文字图层只改换行宽度（高度随内容变化）；直线只给 width 时只改长度",
		Params: []Param{REF, {Name: "width", Type: "number", Desc: "新宽度"}, {Name: "height", Type: "number", Desc: "新高度（省略则等比）"}},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			w, h := num(cmd["width"], 0), num(cmd["height"], 0)
			if w <= 0 && h <= 0 {
				return nil, errf("give a width and/or a height")
			}
			isLine := l.Type == "shape" && l.Shape != nil && l.Shape.Kind == "line"
			switch {
			case l.Type == "text":
				if w > 0 {
					l.Width = math.Max(8, round(w))
				}
			case isLine && h <= 0:
				l.Width = math.Max(1, round(w))
			default:
				ratio := l.Width / math.Max(1, l.Height)
				if w > 0 && h <= 0 {
					h = w / ratio
				} else if h > 0 && w <= 0 {
					w = h * ratio
				}
				l.Width, l.Height = math.Max(1, round(w)), math.Max(1, round(h))
			}
			if err := c.measure(); err != nil {
				return nil, err
			}
			return map[string]any{"width": round(l.Width), "height": round(l.Height)}, nil
		},
	})

	define(Spec{
		Type: "rotateLayer", Summary: "旋转图层（角度，顺时针）",
		Params: []Param{REF, {Name: "rotation", Type: "number", Desc: "角度", Required: true}},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			// Turns about the centre, as a hand on the rotate handle does.
			f := FrameOf(l)
			cx, cy := Centre(l, f)
			l.Rotation = normAngle(num(cmd["rotation"], l.Rotation))
			PlaceCentre(l, f, cx, cy)
			l.X, l.Y = round(l.X), round(l.Y)
			return map[string]any{"rotation": l.Rotation}, nil
		},
	})

	define(Spec{
		Type: "updateLayer", Summary: "通用属性补丁：位置/尺寸/旋转/不透明度/显隐/锁定 + 该类型支持的样式键",
		Params: []Param{REF, {Name: "props", Type: "object", Required: true,
			Desc: "要改的属性：name,x,y,width,height,rotation,opacity,visible,locked,color/fill,fontFamily,fontSize,fontWeight,fontStyle,textAlign,lineHeight,charSpacing,shadow,textBackgroundColor,stroke,strokeWidth,paintFirst,underline,linethrough,cornerRadius,flipX,flipY,skew,skewY,stretch,warp,warpAmount,warpBias"}},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			props, _ := cmd["props"].(map[string]any)
			if len(props) == 0 {
				return nil, errf("props is empty")
			}
			if err := applyProps(l, props); err != nil {
				return nil, err
			}
			if has(props, "rotation") {
				l.Rotation = normAngle(l.Rotation)
			}
			if v, ok := props["name"]; ok {
				if n := strings.TrimSpace(str(v)); n != "" && n != l.Name {
					l.Name = c.B.UniqueName(n)
				}
			}
			if err := c.measure(); err != nil {
				return nil, err
			}
			return withIndex(c, l), nil
		},
	})

	define(Spec{
		Type: "setOpacity", Summary: "设置不透明度",
		Params: []Param{REF, {Name: "opacity", Type: "number", Desc: "0-1", Required: true}},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			l.Opacity = clamp(num(cmd["opacity"], l.Opacity), 0, 1)
			return map[string]any{"opacity": l.Opacity}, nil
		},
	})

	define(Spec{
		Type: "setVisible", Summary: "显示 / 隐藏图层",
		Params: []Param{REF, {Name: "visible", Type: "boolean", Desc: "true 显示", Required: true}},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			l.Visible = boolean(cmd["visible"], !l.Visible)
			return map[string]any{"id": l.ID, "name": l.Name, "visible": l.Visible}, nil
		},
	})

	define(Spec{
		Type: "setLocked", Summary: "锁定 / 解锁图层（锁定后拖不动）",
		Params: []Param{REF, {Name: "locked", Type: "boolean", Desc: "true 锁定", Required: true}},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			l.Locked = boolean(cmd["locked"], !l.Locked)
			return map[string]any{"id": l.ID, "name": l.Name, "locked": l.Locked}, nil
		},
	})

	define(Spec{
		Type: "alignLayer", Summary: "把图层对齐到画布的边或中线",
		Params: []Param{REF,
			{Name: "h", Type: "string", Desc: "水平对齐", Enum: []string{"left", "center", "right"}},
			{Name: "v", Type: "string", Desc: "垂直对齐", Enum: []string{"top", "middle", "bottom"}},
		},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			h, v := strings.TrimSpace(str(cmd["h"])), strings.TrimSpace(str(cmd["v"]))
			if h == "" && v == "" {
				return nil, errf("give h and/or v")
			}
			cw, ch := c.B.Canvas.Width, c.B.Canvas.Height
			switch h {
			case "left":
				l.X = 0
			case "center":
				l.X = round((cw - l.Width) / 2)
			case "right":
				l.X = round(cw - l.Width)
			}
			switch v {
			case "top":
				l.Y = 0
			case "middle":
				l.Y = round((ch - l.Height) / 2)
			case "bottom":
				l.Y = round(ch - l.Height)
			}
			return position(l), nil
		},
	})

	define(Spec{
		Type: "fitToCanvas", Summary: "让图层适配画布",
		Params: []Param{REF, {Name: "mode", Type: "string", Default: "contain", Enum: []string{"contain", "cover", "width", "height"},
			Desc: "contain 完整放入并居中 / cover 铺满裁切 / width 按宽铺满 / height 按高铺满"}},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			mode := pick(cmd["mode"], []string{"contain", "cover", "width", "height"}, "contain")
			f := FrameOf(l)
			nw, nh := f.CW, f.CH
			cw, ch := c.B.Canvas.Width, c.B.Canvas.Height
			var w, h float64
			switch mode {
			case "contain", "cover":
				s := math.Min(cw/nw, ch/nh)
				if mode == "cover" {
					s = math.Max(cw/nw, ch/nh)
				}
				w, h = nw*s, nh*s
			case "width":
				w, h = cw, nh*cw/nw
			default:
				w, h = nw*ch/nh, ch
			}
			switch l.Type {
			case "image", "group":
				l.Width, l.Height = w, h
			case "shape":
				l.Width, l.Height = round(w), round(h)
			default:
				l.Width = math.Max(8, round(w)) * f.ScaleX
				if err := c.measure(); err != nil {
					return nil, err
				}
			}
			switch mode {
			case "contain", "cover":
				l.X, l.Y = round((cw-l.Width)/2), round((ch-l.Height)/2)
			case "width":
				l.X, l.Y = 0, round((ch-l.Height)/2)
			default:
				l.X, l.Y = round((cw-l.Width)/2), 0
			}
			return position(l), nil
		},
	})

	zmove := func(typ, summary string, to func(i, n int) int) {
		define(Spec{Type: typ, Summary: summary, Params: []Param{REF},
			Run: func(c *Ctx, cmd map[string]any) (any, error) {
				l, err := c.layer(cmd["id"])
				if err != nil {
					return nil, err
				}
				i := c.B.Index(l.ID)
				n := len(c.B.Layers)
				c.remove(l)
				c.insert(l, int(clamp(float64(to(i, n)), 0, float64(n-1))))
				return map[string]any{"index": c.B.Index(l.ID)}, nil
			}})
	}
	zmove("bringForward", "图层上移一层", func(i, _ int) int { return i + 1 })
	zmove("sendBackward", "图层下移一层", func(i, _ int) int { return i - 1 })
	zmove("bringToFront", "置顶", func(_, n int) int { return n - 1 })
	zmove("sendToBack", "置底", func(_, _ int) int { return 0 })

	define(Spec{
		Type: "setZIndex", Summary: "把图层放到指定层级（0 = 最底层，最大 = 最顶层）",
		Params: []Param{REF, {Name: "index", Type: "number", Required: true, Desc: "目标层级，0 起算，0 是最底层；此值即 listLayers 返回的 index 字段"}},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			n := len(c.B.Layers)
			at := int(clamp(round(num(cmd["index"], float64(c.B.Index(l.ID)))), 0, float64(n-1)))
			c.remove(l)
			c.insert(l, at)
			return map[string]any{"index": c.B.Index(l.ID), "of": n}, nil
		},
	})

	define(Spec{
		Type: "reorderLayers", Summary: "整栈重排：按给出的顺序摆放图层（第一个在最底层）",
		Params:  []Param{{Name: "ids", Type: "array", Required: true, Desc: "图层 id 或名字的数组，从底到顶；没列出的图层保持原相对顺序排在它们上面"}},
		Returns: "{order: 从底到顶的图层名, of: 图层总数}",
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			if l, _ := cmd["ids"].([]any); len(l) == 0 {
				return nil, errf(`reorderLayers needs a non-empty "ids" array, e.g. ["背景", "人像", "标题"]`)
			}
			named, err := c.layers(cmd["ids"], 1)
			if err != nil {
				return nil, err
			}
			for i, l := range named {
				c.remove(l)
				c.insert(l, i)
			}
			order := make([]string, len(c.B.Layers))
			for i, l := range c.B.Layers {
				order[i] = l.Name
			}
			return map[string]any{"order": order, "of": len(order)}, nil
		},
	})
}

// withIndex is a layer plus its stacking index, as getLayer answers.
func withIndex(c *Ctx, l *Layer) map[string]any {
	raw := l.Clone()
	m := map[string]any{}
	b, _ := jsonMarshal(raw)
	_ = jsonUnmarshal(b, &m)
	m["index"] = c.B.Index(l.ID)
	return m
}
