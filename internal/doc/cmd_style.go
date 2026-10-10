package doc

import (
	"math"
	"strings"
)

func init() {
	define(Spec{
		Type: "setText", Summary: "修改文字内容",
		Params: []Param{REF, {Name: "text", Type: "string", Desc: `新文字，支持 \n`, Required: true}},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			if l.Type != "text" {
				return nil, errf(`layer "%s" is a %s, not text`, l.Name, l.Type)
			}
			t := str(cmd["text"])
			l.Text = &t
			return map[string]any{"id": l.ID, "text": t}, nil
		},
	})

	define(Spec{
		Type: "setTextStyle", Summary: "修改文字样式：字体/字号/颜色/行高/字距/阴影/底色/描边/下划线",
		Params: []Param{REF, {Name: "style", Type: "object", Required: true,
			Desc: `fontFamily,fontSize,fontWeight,fontStyle,fill|color,textAlign,lineHeight,charSpacing,shadow("color:blur:offX:offY"),textBackgroundColor,stroke("color:width"),paintFirst,underline,linethrough,padding,skew(倾斜角度，正值向右倒),skewY(纵向斜切角度，正值右端抬高、整行斜向上走，竖笔仍竖直),stretch(横向拉伸倍数，0.8 压窄 1.2 拉宽),warp(变形：none 或 trapezoid 梯形，右端收窄),warpAmount(梯形强度 -100~100),warpBias(相对高度 -100~100：-100 下沿平直，100 上沿平直)`}},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			if l.Type != "text" {
				return nil, errf(`layer "%s" is a %s, not text`, l.Name, l.Type)
			}
			style, _ := cmd["style"].(map[string]any)
			if err := applyProps(l, style); err != nil {
				return nil, err
			}
			if err := c.measure(); err != nil {
				return nil, err
			}
			return withIndex(c, l), nil
		},
	})

	define(Spec{
		Type: "applyTextPreset", Summary: "套用封面文字预设（黄底黑字/黑描边/荧光笔…）",
		Params: []Param{REF, {Name: "preset", Type: "string", Desc: "预设 key", Enum: textPresetKeys(), Required: true}},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			key := strings.TrimSpace(str(cmd["preset"]))
			p := textPresetByKey(key)
			if p == nil {
				return nil, errf(`unknown text preset "%s" — try one of %s`, key, strings.Join(textPresetKeys(), ", "))
			}
			props := map[string]any{}
			for k, v := range p.Style {
				props[k] = v
			}
			// The width goes after the colour: "stroke" then "strokeWidth".
			sw, hasSW := props["strokeWidth"]
			delete(props, "strokeWidth")
			if err := applyProps(l, props); err != nil {
				return nil, err
			}
			if hasSW {
				_ = applyProps(l, map[string]any{"strokeWidth": sw})
			}
			if err := c.measure(); err != nil {
				return nil, err
			}
			return withIndex(c, l), nil
		},
	})

	define(Spec{
		Type: "setImageProps", Summary: "修改图片：翻转、圆角、描边、投影、滤镜（亮度/对比度/饱和度/模糊/黑白/复古）",
		Params: []Param{REF,
			{Name: "flipX", Type: "boolean", Desc: "水平翻转"},
			{Name: "flipY", Type: "boolean", Desc: "垂直翻转"},
			{Name: "cornerRadius", Type: "number", Desc: "圆角像素"},
			{Name: "filters", Type: "object", Desc: "滤镜值：brightness(-1..1) contrast(-1..1) saturation(-1..1) blur(0..1) grayscale(0|1) sepia(0|1)"},
			{Name: "stroke", Type: "string", Desc: `描边颜色（"颜色" 或 "颜色:粗细"），"none" 去掉。沿图片不透明的边缘描一圈：抠过背景的人像就是贴纸式白边，普通照片就是外框`},
			{Name: "strokeWidth", Type: "number", Desc: "描边粗细（画布像素），0 去掉"},
			{Name: "shadow", Type: "string", Desc: `投影 "颜色:模糊:X偏移:Y偏移"（画布像素），如 "rgba(0,0,0,0.35):40:0:16"；"none" 去掉。抠过背景的图，投影跟着主体轮廓走`},
		},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			if l.Type != "image" {
				return nil, errf(`layer "%s" is a %s, not an image`, l.Name, l.Type)
			}
			if l.Image == nil || l.Image.URL == "" {
				return nil, errf(`layer "%s" 的图片引用已丢失，先用 replaceImage 重新指定一张图`, l.Name)
			}
			for _, k := range []string{"flipX", "flipY"} {
				if has(cmd, k) {
					l.SetS(k, boolean(cmd[k], boolean(l.S(k), false)))
				}
			}
			if has(cmd, "cornerRadius") {
				l.SetS("cornerRadius", nilIfZero(math.Max(0, round(num(cmd["cornerRadius"], 0)))))
			}
			if has(cmd, "filters") {
				setFilters(l, cmd["filters"])
			}
			for _, k := range []string{"stroke", "strokeWidth"} {
				if has(cmd, k) {
					v := cmd[k]
					if k == "stroke" && strings.TrimSpace(str(v)) == "none" {
						v = ""
					}
					if err := applyProps(l, map[string]any{k: v}); err != nil {
						return nil, err
					}
				}
			}
			if w, ok := l.S("strokeWidth").(float64); ok && w <= 0 {
				l.SetS("stroke", nil)
				l.SetS("strokeWidth", nil)
			}
			if has(cmd, "shadow") {
				if err := applyProps(l, map[string]any{"shadow": cmd["shadow"]}); err != nil {
					return nil, err
				}
			}
			return withIndex(c, l), nil
		},
	})

	define(Spec{
		Type: "replaceImage", Summary: "替换图片内容：新图按原比例放进原图层的框里并居中（不拉伸）",
		Params: []Param{REF, {Name: "url", Type: "string", Desc: "新图片 url"}, {Name: "path", Type: "string", Desc: "新图片本机路径"}},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			if l.Type != "image" {
				return nil, errf(`layer "%s" is a %s, not an image`, l.Name, l.Type)
			}
			ref := firstNonEmpty(str(cmd["url"]), str(cmd["path"]))
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
			if l.Image == nil || l.Image.URL == "" {
				// A lost picture: the new one fills the box it left behind.
				l.Image = &ImageRef{URL: url, OriginalWidth: float64(nw), OriginalHeight: float64(nh)}
				return withIndex(c, l), nil
			}
			old := FrameOf(l)
			cx, cy := Centre(l, old)
			k := math.Min(l.Width/float64(nw), l.Height/float64(nh))
			l.Image = &ImageRef{URL: url, OriginalWidth: float64(nw), OriginalHeight: float64(nh)}
			l.Width, l.Height = float64(nw)*k, float64(nh)*k
			PlaceCentre(l, FrameOf(l), cx, cy)
			l.X, l.Y = round(l.X), round(l.Y)
			return withIndex(c, l), nil
		},
	})

	define(Spec{
		Type: "removeBackground", Summary: "去除图片背景（抠图：装了 rembg 用 BiRefNet，否则用 macOS 自带的主体抠图）：主体保留、背景透明，图层位置和大小不变；restore 恢复原图",
		Params: []Param{REF,
			{Name: "model", Type: "string", Desc: "general 通用（物品、插画、Logo）/ portrait 人像（头发边缘更细）", Enum: []string{"general", "portrait"}, Default: "general"},
			{Name: "restore", Type: "boolean", Desc: "true = 换回去背景之前的原图", Default: false},
		},
		Returns: "{ id, name, url, model }",
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			if l.Type != "image" {
				return nil, errf(`layer "%s" is a %s, not an image`, l.Name, l.Type)
			}
			if l.Image == nil || l.Image.URL == "" {
				return nil, errf(`layer "%s" 的图片引用已丢失，先用 replaceImage 重新指定一张图`, l.Name)
			}
			if boolean(cmd["restore"], false) {
				if l.Image.Original == "" {
					return nil, errf(`layer "%s" 没有去过背景，没有原图可恢复`, l.Name)
				}
				l.Image.URL, l.Image.SHA, l.Image.Original = l.Image.Original, "", ""
				return map[string]any{"id": l.ID, "name": l.Name, "url": l.Image.URL}, nil
			}
			model := pick(cmd["model"], []string{"general", "portrait"}, "general")
			// Always cut from the original, so trying the other model does not
			// cut an already cut-out picture.
			source := l.Image.URL
			if l.Image.Original != "" {
				source = l.Image.Original
			}
			url, w, h, err := c.svc().RemoveBackground(source, "birefnet-"+model)
			if err != nil {
				return nil, err
			}
			// The cut-out has the source's pixel size, so the box, crop and
			// position all still fit it.
			if w > 0 && h > 0 {
				l.Image.OriginalWidth, l.Image.OriginalHeight = float64(w), float64(h)
			}
			l.Image.Original = source
			l.Image.URL, l.Image.SHA = url, ""
			return map[string]any{"id": l.ID, "name": l.Name, "url": url, "model": model}, nil
		},
	})

	define(Spec{
		Type: "setShapeProps", Summary: "修改形状：填充、描边、圆角；直线的颜色（stroke）、粗细（strokeWidth）、线型、箭头",
		Params: []Param{REF,
			{Name: "fill", Type: "string", Desc: "填充色"},
			{Name: "stroke", Type: "string", Desc: "描边色（直线：线的颜色）"},
			{Name: "strokeWidth", Type: "number", Desc: "描边宽度（直线：线的粗细）"},
			{Name: "cornerRadius", Type: "number", Desc: "圆角"},
			{Name: "lineStyle", Type: "string", Desc: "直线：solid / dashed", Enum: []string{"solid", "dashed"}},
			{Name: "arrow", Type: "string", Desc: "直线：none / end / both", Enum: []string{"none", "end", "both"}},
		},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			if l.Type != "shape" {
				return nil, errf(`layer "%s" is a %s, not a shape`, l.Name, l.Type)
			}
			line := l.Shape != nil && l.Shape.Kind == "line"
			if has(cmd, "fill") {
				if line && !has(cmd, "stroke") {
					l.SetS("stroke", colour(cmd["fill"], str(l.S("stroke"))))
				} else if !line {
					l.SetS("fill", colour(cmd["fill"], str(l.S("fill"))))
				}
			}
			if has(cmd, "stroke") {
				if s := strings.TrimSpace(str(cmd["stroke"])); s != "" {
					l.SetS("stroke", s)
				} else {
					l.SetS("stroke", nil)
				}
			}
			if has(cmd, "strokeWidth") {
				w := math.Max(0, num(cmd["strokeWidth"], 1))
				if w == 0 {
					l.SetS("strokeWidth", nil)
				} else {
					l.SetS("strokeWidth", w)
				}
			}
			if has(cmd, "cornerRadius") {
				l.SetS("cornerRadius", nilIfZero(math.Max(0, round(num(cmd["cornerRadius"], 0)))))
			}
			if line {
				if has(cmd, "lineStyle") {
					l.SetS("lineStyle", nilIf(pick(cmd["lineStyle"], []string{"solid", "dashed"}, "solid"), "solid"))
				}
				if has(cmd, "arrow") {
					l.SetS("arrow", nilIf(pick(cmd["arrow"], []string{"none", "end", "both"}, "none"), "none"))
				}
				// A thicker line needs a taller box for its arrowheads.
				if need := LineBoxHeight(num(l.S("strokeWidth"), 1)); l.Height < need {
					f := FrameOf(l)
					cx, cy := Centre(l, f)
					l.Height = need
					PlaceCentre(l, FrameOf(l), cx, cy)
				}
			}
			return withIndex(c, l), nil
		},
	})

	define(Spec{
		Type: "cropImage", Summary: "裁剪图片：只显示原图中的一个矩形区域（单位是原图像素）；reset 恢复完整图片",
		Params: []Param{REF,
			{Name: "x", Type: "number", Desc: "裁剪框左上角 x（原图像素）"},
			{Name: "y", Type: "number", Desc: "裁剪框左上角 y（原图像素）"},
			{Name: "width", Type: "number", Desc: "裁剪框宽（原图像素）"},
			{Name: "height", Type: "number", Desc: "裁剪框高（原图像素）"},
			{Name: "reset", Type: "boolean", Desc: "true 取消裁剪", Default: false},
		},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			if l.Type != "image" {
				return nil, errf(`layer "%s" is a %s, not an image`, l.Name, l.Type)
			}
			if l.Image == nil || l.Image.URL == "" {
				return nil, errf(`layer "%s" 的图片引用已丢失，先用 replaceImage 重新指定一张图`, l.Name)
			}
			natW, natH := l.Image.OriginalWidth, l.Image.OriginalHeight
			f := FrameOf(l)
			if natW <= 0 {
				natW, natH = f.CW, f.CH
			}
			x, y, w, h := 0.0, 0.0, natW, natH
			if !boolean(cmd["reset"], false) {
				if !has(cmd, "width") || !has(cmd, "height") {
					return nil, errf("give x, y, width and height (in the image's own pixels), or reset: true")
				}
				x = clamp(round(num(cmd["x"], 0)), 0, natW-1)
				y = clamp(round(num(cmd["y"], 0)), 0, natH-1)
				w = clamp(round(num(cmd["width"], natW)), 1, natW-x)
				h = clamp(round(num(cmd["height"], natH)), 1, natH-y)
			}
			// The pixels that stay stay where they are on the board: move the
			// layer by how far the window's corner moved, along its rotation.
			var cropX, cropY float64
			if l.Image.Crop != nil {
				cropX, cropY = l.Image.Crop.X, l.Image.Crop.Y
			}
			sx, sy := f.ScaleX, f.ScaleY
			flipX, flipY := boolean(l.S("flipX"), false), boolean(l.S("flipY"), false)
			sign := func(b bool) float64 {
				if b {
					return -1
				}
				return 1
			}
			ddx := (x - cropX) * sx * sign(flipX)
			ddy := (y - cropY) * sy * sign(flipY)
			fx, fy := 0.0, 0.0
			if flipX {
				fx = (f.CW - w) * sx
			}
			if flipY {
				fy = (f.CH - h) * sy
			}
			mx, my := ddx+fx, ddy+fy
			r := l.Rotation * math.Pi / 180
			l.X = round(l.X + mx*math.Cos(r) - my*math.Sin(r))
			l.Y = round(l.Y + mx*math.Sin(r) + my*math.Cos(r))
			if x == 0 && y == 0 && w >= natW && h >= natH {
				l.Image.Crop = nil
			} else {
				l.Image.Crop = &CropRect{X: x, Y: y, Width: w, Height: h}
			}
			l.Width, l.Height = w*sx, h*sy
			return withIndex(c, l), nil
		},
	})
}
