package doc

import (
	"math"
	"path"
	"strconv"
	"strings"
)

// DefaultFamily is the face new text starts in.
var DefaultFamily = "PingFang SC"

func init() {
	define(Spec{
		Type: "getDocument", Summary: "读取当前画板的文档（画布尺寸、背景、全部图层）；整个项目用 getProject",
		Returns: "Document", ReadOnly: true,
		Run: func(c *Ctx, _ map[string]any) (any, error) {
			return map[string]any{"version": 1, "canvas": c.B.Canvas, "layers": c.B.Layers}, nil
		},
	})

	define(Spec{
		Type: "setCanvasSize", Summary: "设置画布宽高（可用预设 key 一步到位）",
		Params: []Param{
			{Name: "width", Type: "number", Desc: "宽，像素", Default: 1242},
			{Name: "height", Type: "number", Desc: "高，像素", Default: 1656},
			{Name: "preset", Type: "string", Desc: "画布预设 key，给了就忽略 width/height", Enum: canvasPresetKeys()},
		},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			w, h := round(num(cmd["width"], c.B.Canvas.Width)), round(num(cmd["height"], c.B.Canvas.Height))
			if key := strings.TrimSpace(str(cmd["preset"])); key != "" {
				p := CanvasPresetByKey(key)
				if p == nil {
					return nil, errf(`unknown canvas preset "%s" — try one of %s`, key, strings.Join(canvasPresetKeys(), ", "))
				}
				w, h = p.Width, p.Height
			}
			if w < 1 || h < 1 {
				return nil, errf("canvas must be at least 1×1")
			}
			c.B.Canvas.Width, c.B.Canvas.Height = clamp(w, 1, 20000), clamp(h, 1, 20000)
			return map[string]any{"width": c.B.Canvas.Width, "height": c.B.Canvas.Height}, nil
		},
	})

	define(Spec{
		Type: "setBackground", Summary: "设置画布背景：颜色或图片",
		Params: []Param{
			{Name: "color", Type: "string", Desc: `CSS 颜色，如 "#000000"、transparent`, Default: "#ffffff"},
			{Name: "image", Type: "string", Desc: "图片 url / 本机路径（cover 方式铺满）"},
		},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			if has(cmd, "image") {
				ref := strings.TrimSpace(str(cmd["image"]))
				if ref == "" {
					return nil, errf("image is empty")
				}
				url, _, _, err := c.svc().Asset(ref)
				if err != nil {
					return nil, err
				}
				c.B.Canvas.BackgroundImage, c.B.Canvas.Background = url, ""
			} else {
				c.B.Canvas.Background, c.B.Canvas.BackgroundImage = colour(cmd["color"], "#ffffff"), ""
			}
			return map[string]any{"background": c.B.Canvas.Background, "backgroundImage": c.B.Canvas.BackgroundImage}, nil
		},
	})

	define(Spec{
		Type: "addText", Summary: "添加文字图层（默认放在画布中部，颜色按背景自动选深/浅）",
		Params: []Param{
			{Name: "text", Type: "string", Desc: `文字内容，支持 \n 换行`, Required: true},
			{Name: "x", Type: "number", Desc: "左上角 x，默认居中", Default: 0},
			{Name: "y", Type: "number", Desc: "左上角 y，默认居中", Default: 0},
			{Name: "width", Type: "number", Desc: "文本框宽度（换行宽度）", Default: 600},
			{Name: "style", Type: "object", Desc: "字体/颜色/阴影等，键见 setTextStyle", Default: map[string]any{"fontSize": 120, "fill": "#111111", "fontWeight": 700}},
			{Name: "name", Type: "string", Desc: "图层名，重名自动加后缀"},
		},
		Returns: "Layer",
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			text := str(cmd["text"])
			if text == "" {
				return nil, errf("text is empty")
			}
			cw, ch := c.B.Canvas.Width, c.B.Canvas.Height
			size := defaultSize(c.B)
			width := clamp(round(num(cmd["width"], round(cw*0.8))), 8, 20000)
			given, _ := cmd["style"].(map[string]any)
			family := DefaultFamily
			style, err := textStyle(given, map[string]any{
				"fontFamily": family,
				"fontSize":   size,
				"fontWeight": 700.0,
				"lineHeight": 1.25,
				"fill":       defaultTextFill(c.B),
			})
			if err != nil {
				return nil, err
			}
			name := strings.TrimSpace(str(cmd["name"]))
			if name == "" {
				name = firstLine(text, 12)
			}
			l := &Layer{
				ID: NewID("t"), Name: c.B.UniqueName(name), Type: "text",
				X: round(num(cmd["x"], (cw-width)/2)), Y: round(num(cmd["y"], (ch-size*1.4)/2)),
				Width: width, Height: round(size * 1.4), Opacity: 1, Visible: true,
				Text: &text, Style: style,
			}
			c.B.Layers = append(c.B.Layers, l)
			if err := c.measure(); err != nil {
				return nil, err
			}
			return map[string]any{"id": l.ID, "name": l.Name, "type": l.Type, "height": round(l.Height)}, nil
		},
	})

	define(Spec{
		Type: "addImage", Summary: "添加图片图层（url / 本机路径均可）",
		Params: []Param{
			{Name: "url", Type: "string", Desc: "图片 url，或已上传的 /assets/xxx.png"},
			{Name: "path", Type: "string", Desc: "本机图片路径（别名 file）"},
			{Name: "file", Type: "string", Desc: "同 path"},
			{Name: "x", Type: "number", Desc: "左上角 x，默认居中"},
			{Name: "y", Type: "number", Desc: "左上角 y，默认居中"},
			{Name: "width", Type: "number", Desc: "显示宽，默认用原始尺寸"},
			{Name: "height", Type: "number", Desc: "显示高，默认按宽等比"},
			{Name: "name", Type: "string", Desc: "图层名"},
		},
		Returns: "Layer",
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			ref := firstNonEmpty(str(cmd["url"]), str(cmd["path"]), str(cmd["file"]))
			if ref == "" {
				return nil, errf("give a url or a path")
			}
			url, nw, nh, err := c.svc().Asset(ref)
			if err != nil {
				return nil, err
			}
			if nw <= 0 || nh <= 0 {
				return nil, errf("could not load image: %s", ref)
			}
			w, h := num(cmd["width"], 0), num(cmd["height"], 0)
			width, height := float64(nw), float64(nh)
			switch {
			case w > 0 && h > 0:
				width, height = w, h
			case w > 0:
				width, height = w, round(w*float64(nh)/float64(nw))
			case h > 0:
				width, height = round(h*float64(nw)/float64(nh)), h
			}
			name := strings.TrimSpace(str(cmd["name"]))
			if name == "" {
				name = baseName(ref)
			}
			l := &Layer{
				ID: NewID("i"), Name: c.B.UniqueName(name), Type: "image",
				X: round(num(cmd["x"], (c.B.Canvas.Width-width)/2)), Y: round(num(cmd["y"], (c.B.Canvas.Height-height)/2)),
				Width: width, Height: height, Opacity: 1, Visible: true,
				Image: &ImageRef{URL: url, OriginalWidth: float64(nw), OriginalHeight: float64(nh)},
				Style: map[string]any{"flipX": false, "flipY": false},
			}
			c.B.Layers = append(c.B.Layers, l)
			return map[string]any{"id": l.ID, "name": l.Name, "type": l.Type, "width": width, "height": height}, nil
		},
	})

	define(Spec{
		Type: "addShape", Summary: "添加形状图层（矩形/椭圆/三角形/直线）。直线：沿宽度方向画在框的中线上，stroke 是颜色、strokeWidth 是粗细，旋转改方向",
		Params: []Param{
			{Name: "kind", Type: "string", Desc: "形状", Enum: []string{"rect", "ellipse", "triangle", "line"}, Default: "rect"},
			{Name: "x", Type: "number", Desc: "左上角 x", Required: true},
			{Name: "y", Type: "number", Desc: "左上角 y", Required: true},
			{Name: "width", Type: "number", Desc: "宽", Required: true},
			{Name: "height", Type: "number", Desc: "高", Required: true},
			{Name: "fill", Type: "string", Desc: "填充色", Default: "#e5322d"},
			{Name: "stroke", Type: "string", Desc: "描边色"},
			{Name: "strokeWidth", Type: "number", Desc: "描边宽度"},
			{Name: "cornerRadius", Type: "number", Desc: "圆角（矩形）"},
			{Name: "lineStyle", Type: "string", Desc: "直线：solid 实线 / dashed 虚线", Enum: []string{"solid", "dashed"}},
			{Name: "arrow", Type: "string", Desc: "直线：none / end 单向箭头（右端）/ both 双向箭头", Enum: []string{"none", "end", "both"}},
			{Name: "opacity", Type: "number", Desc: "不透明度 0-1", Default: 1},
			{Name: "name", Type: "string", Desc: "图层名"},
		},
		Returns: "Layer",
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			kind := pick(cmd["kind"], []string{"rect", "ellipse", "triangle", "line"}, "rect")
			line := kind == "line"
			width := round(num(cmd["width"], 0))
			sw := 0.0
			if line {
				sw = math.Max(1, num(cmd["strokeWidth"], 6))
			}
			height := round(num(cmd["height"], 0))
			if line && !has(cmd, "height") {
				height = LineBoxHeight(sw)
			}
			if width <= 0 || height <= 0 {
				return nil, errf("width and height must be positive")
			}
			style := map[string]any{}
			if line {
				stroke := cmd["stroke"]
				if stroke == nil {
					stroke = cmd["fill"]
				}
				style["stroke"] = colour(stroke, "#1c1b18")
				style["strokeWidth"] = sw
				if pick(cmd["lineStyle"], []string{"solid", "dashed"}, "solid") == "dashed" {
					style["lineStyle"] = "dashed"
				}
				if a := pick(cmd["arrow"], []string{"none", "end", "both"}, "none"); a != "none" {
					style["arrow"] = a
				}
			} else {
				style["fill"] = colour(cmd["fill"], "#e5322d")
				if has(cmd, "stroke") {
					style["stroke"] = colour(cmd["stroke"], "")
				}
				if has(cmd, "strokeWidth") {
					style["strokeWidth"] = num(cmd["strokeWidth"], 1)
				}
				if has(cmd, "cornerRadius") {
					if r := num(cmd["cornerRadius"], 0); r > 0 {
						style["cornerRadius"] = round(r)
					}
				}
			}
			name := strings.TrimSpace(str(cmd["name"]))
			if name == "" {
				name = shapeName(kind)
			}
			l := &Layer{
				ID: NewID("s"), Name: c.B.UniqueName(name), Type: "shape",
				X: round(num(cmd["x"], 0)), Y: round(num(cmd["y"], 0)), Width: width, Height: height,
				Opacity: clamp(num(cmd["opacity"], 1), 0, 1), Visible: true,
				Shape: &ShapeRef{Kind: kind}, Style: style,
			}
			c.B.Layers = append(c.B.Layers, l)
			return map[string]any{"id": l.ID, "name": l.Name, "type": l.Type}, nil
		},
	})

	define(Spec{
		Type: "duplicateLayer", Summary: "复制一个图层，副本放在原图层上方并略作偏移",
		Params: []Param{REF,
			{Name: "x", Type: "number", Desc: "副本左上角 x，默认在原位置右下偏移"},
			{Name: "y", Type: "number", Desc: "副本左上角 y，默认在原位置右下偏移"},
		},
		Returns: "Layer",
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			src, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			cp := freshIDs(src.Clone())
			cp.Name = c.B.UniqueName(src.Name + " 副本")
			cp.X += round(math.Min(48, math.Max(16, cp.Width*0.05)))
			cp.Y += round(math.Min(48, math.Max(16, cp.Height*0.05)))
			if has(cmd, "x") {
				cp.X = round(num(cmd["x"], cp.X))
			}
			if has(cmd, "y") {
				cp.Y = round(num(cmd["y"], cp.Y))
			}
			c.insert(cp, c.B.Index(src.ID)+1)
			return map[string]any{"id": cp.ID, "name": cp.Name, "type": cp.Type}, nil
		},
	})

	define(Spec{
		Type: "removeLayer", Summary: "删除图层", Params: []Param{REF},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			c.remove(l)
			return map[string]any{"removed": l.ID, "name": l.Name}, nil
		},
	})

	define(Spec{
		Type: "renameLayer", Summary: "重命名图层（名字在文档内唯一）",
		Params: []Param{REF, {Name: "name", Type: "string", Desc: "新名字", Required: true}},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			name := strings.TrimSpace(str(cmd["name"]))
			if name == "" {
				return nil, errf("name is empty")
			}
			if name != l.Name {
				l.Name = c.B.UniqueName(name)
			}
			return map[string]any{"id": l.ID, "name": l.Name}, nil
		},
	})
}

// --- helpers ---------------------------------------------------------------------

// LineBoxHeight is a line's box height: room for its thickness and arrowheads.
func LineBoxHeight(strokeWidth float64) float64 {
	return math.Max(24, math.Ceil(math.Max(strokeWidth*3.2, 12)*1.4))
}

func defaultSize(b *Board) float64 {
	return round(math.Min(b.Canvas.Width, b.Canvas.Height) / 8)
}

// defaultTextFill is dark on a light canvas, light on a dark or photo one.
func defaultTextFill(b *Board) string {
	bg := strings.TrimSpace(b.Canvas.Background)
	if bg == "" || bg == "transparent" || !strings.HasPrefix(bg, "#") {
		return "#ffffff"
	}
	h := bg[1:]
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		return "#ffffff"
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return "#ffffff"
	}
	r, g, bl := float64(v>>16&0xff), float64(v>>8&0xff), float64(v&0xff)
	if (0.299*r+0.587*g+0.114*bl)/255 > 0.55 {
		return "#111111"
	}
	return "#ffffff"
}

func firstLine(text string, max int) string {
	line := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	r := []rune(line)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	if line == "" {
		return "文字"
	}
	return line
}

func shapeName(kind string) string {
	switch kind {
	case "ellipse":
		return "椭圆"
	case "triangle":
		return "三角形"
	case "line":
		return "直线"
	}
	return "矩形"
}

func baseName(ref string) string {
	ref = strings.SplitN(strings.SplitN(ref, "?", 2)[0], "#", 2)[0]
	b := path.Base(ref)
	if i := strings.LastIndex(b, "."); i > 0 {
		b = b[:i]
	}
	if b == "" || b == "." || b == "/" {
		return "图片"
	}
	return b
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

func prefixOf(t string) string {
	switch t {
	case "text":
		return "t"
	case "image":
		return "i"
	case "group":
		return "g"
	}
	return "s"
}

// freshIDs gives a layer (and a group's members) ids no other layer has.
func freshIDs(l *Layer) *Layer {
	l.ID = NewID(prefixOf(l.Type))
	for _, c := range l.Children {
		freshIDs(c)
	}
	return l
}
