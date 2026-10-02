package doc

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"

	_ "golang.org/x/image/webp"
)

func init() {
	define(Spec{
		Type: "listLayers", Summary: "列出全部图层（从最上层到最底层）",
		Returns: "{ layers: LayerRow[], count } — front to back", ReadOnly: true,
		Run: func(c *Ctx, _ map[string]any) (any, error) {
			rows := Rows(c.B)
			return map[string]any{"count": len(rows), "order": "front to back", "layers": rows}, nil
		},
	})

	define(Spec{
		Type: "getLayer", Summary: "读取单个图层的完整信息", Params: []Param{REF}, Returns: "Layer", ReadOnly: true,
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			l, err := c.layer(cmd["id"])
			if err != nil {
				return nil, err
			}
			return withIndex(c, l), nil
		},
	})

	define(Spec{
		Type: "listFonts", Summary: "列出可用字体族", Returns: "{ fonts: string[] }", ReadOnly: true,
		Run: func(c *Ctx, _ map[string]any) (any, error) {
			f := c.svc().Fonts()
			return map[string]any{"fonts": f, "count": len(f)}, nil
		},
	})

	define(Spec{
		Type: "listTextPresets", Summary: "列出文字预设", Returns: "{ presets }", ReadOnly: true,
		Run: func(*Ctx, map[string]any) (any, error) { return map[string]any{"presets": TextPresets}, nil },
	})

	define(Spec{
		Type: "listCanvasPresets", Summary: "列出画布预设", Returns: "{ presets }", ReadOnly: true,
		Run: func(*Ctx, map[string]any) (any, error) { return map[string]any{"presets": CanvasPresets}, nil },
	})

	define(Spec{
		Type: "undo", Summary: "撤销上一步（人类 ⌘Z 与 AI 调用共用同一个历史栈）", Returns: "{ changed }", OwnHistory: true,
		Run: func(c *Ctx, _ map[string]any) (any, error) { return c.e.step(true) },
	})

	define(Spec{
		Type: "redo", Summary: "重做刚被撤销的一步", Returns: "{ changed }", OwnHistory: true,
		Run: func(c *Ctx, _ map[string]any) (any, error) { return c.e.step(false) },
	})

	define(Spec{
		Type: "export", Summary: "导出当前画板为图片，返回 dataURL",
		Params: []Param{
			{Name: "format", Type: "string", Desc: "png|jpeg|webp", Default: "png", Enum: []string{"png", "jpeg", "webp"}},
			{Name: "multiplier", Type: "number", Desc: "倍率，2 就是两倍图", Default: 1},
			{Name: "quality", Type: "number", Desc: "jpeg/webp 质量 0-1", Default: 0.92},
			{Name: "transparent", Type: "boolean", Desc: "不画背景色/背景图（png/webp 得到透明底）", Default: false},
			{Name: "ids", Type: "array", Desc: "只导出这些图层（id 或名字），画面裁到它们的外框"},
		},
		Returns: "{ dataURL, width, height, format }", ReadOnly: true,
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			format := pick(cmd["format"], []string{"png", "jpeg", "jpg", "webp"}, "png")
			if format == "jpg" {
				format = "jpeg"
			}
			opts := RenderOptions{
				Scale:       clamp(num(cmd["multiplier"], 1), 0.05, 8),
				Format:      format,
				Quality:     clamp(num(cmd["quality"], 0.92), 0.05, 1),
				Transparent: boolean(cmd["transparent"], false),
			}
			if has(cmd, "ids") {
				ls, err := c.layers(cmd["ids"], 1)
				if err != nil {
					return nil, err
				}
				for _, l := range ls {
					opts.IDs = append(opts.IDs, l.ID)
				}
			}
			b, err := c.svc().Render(c.B, opts)
			if err != nil {
				return nil, err
			}
			cfg, _, _ := image.DecodeConfig(bytes.NewReader(b))
			mime := "image/" + format
			return map[string]any{
				"dataURL": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b),
				"width":   cfg.Width, "height": cfg.Height, "format": format,
			}, nil
		},
	})

	// --- boards ---------------------------------------------------------------

	boardRef := Param{Name: "id", Type: "string", Desc: "画板 id、画板名，或从 1 开始的序号", Required: true}
	row := func(c *Ctx, b *Board) map[string]any {
		_, i := c.P.FindBoard(b.ID)
		return map[string]any{"index": i + 1, "id": b.ID, "name": b.Name, "width": b.Canvas.Width, "height": b.Canvas.Height, "layers": len(b.Layers), "active": b.ID == c.P.Active}
	}
	board := func(c *Ctx, ref any) (*Board, error) {
		b, _ := c.P.FindBoard(str(ref))
		if b == nil {
			return nil, errf(`no board "%s" — boards: %s`, str(ref), c.e.boardList())
		}
		return b, nil
	}
	uniqueBoard := func(p *Project, base string, except string) string {
		taken := map[string]bool{}
		for _, b := range p.Boards {
			if b.ID != except {
				taken[b.Name] = true
			}
		}
		if !taken[base] {
			return base
		}
		for i := 2; i < 500; i++ {
			if n := base + " " + itoa(i); !taken[n] {
				return n
			}
		}
		return base + " " + itoa(len(p.Boards)+1)
	}
	insertBoard := func(p *Project, b *Board, at int) {
		at = int(math.Max(0, math.Min(float64(len(p.Boards)), float64(at))))
		p.Boards = append(p.Boards, nil)
		copy(p.Boards[at+1:], p.Boards[at:])
		p.Boards[at] = b
		p.Active = b.ID
	}

	define(Spec{
		Type: "listBoards", Summary: "列出全部画板（序号从 1 开始、尺寸、图层数、当前是哪个）",
		Returns: "{ active, boards: [{ index, id, name, width, height, layers, active }] }", ReadOnly: true,
		Run: func(c *Ctx, _ map[string]any) (any, error) {
			rows := make([]any, len(c.P.Boards))
			for i, b := range c.P.Boards {
				rows[i] = row(c, b)
			}
			return map[string]any{"active": c.P.Active, "boards": rows}, nil
		},
	})

	define(Spec{
		Type: "getProject", Summary: "读取整个项目：全部画板的文档",
		Returns: "{ version: 2, active, boards: [{ id, name, canvas, layers }] }", ReadOnly: true,
		Run: func(c *Ctx, _ map[string]any) (any, error) { return c.P, nil },
	})

	define(Spec{
		Type: "selectBoard", Summary: "切换到某个画板（之后不带 board 的命令都作用于它）",
		Params: []Param{boardRef}, Returns: "board row", ReadOnly: true,
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			b, err := board(c, cmd["id"])
			if err != nil {
				return nil, err
			}
			c.P.Active = b.ID
			return row(c, b), nil
		},
	})

	define(Spec{
		Type: "addBoard", Summary: "新建画板（默认和当前画板一样大，放在当前画板后面），并切换过去",
		Params: []Param{
			{Name: "name", Type: "string", Desc: `画板名，默认"画板 N"`},
			{Name: "width", Type: "number", Desc: "宽，像素（默认同当前画板）"},
			{Name: "height", Type: "number", Desc: "高，像素（默认同当前画板）"},
			{Name: "preset", Type: "string", Desc: "画布预设 key，给了就忽略 width/height", Enum: canvasPresetKeys()},
			{Name: "background", Type: "string", Desc: "背景色", Default: "#ffffff"},
			{Name: "index", Type: "number", Desc: "放在第几个（从 1 开始）；默认紧跟当前画板"},
		},
		Returns: "board row",
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			cur := c.P.ActiveBoard()
			w, h := round(num(cmd["width"], cur.Canvas.Width)), round(num(cmd["height"], cur.Canvas.Height))
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
			name := strings.TrimSpace(str(cmd["name"]))
			if name == "" {
				for i := len(c.P.Boards) + 1; ; i++ {
					if n := "画板 " + itoa(i); uniqueBoard(c.P, n, "") == n {
						name = n
						break
					}
				}
			} else {
				name = uniqueBoard(c.P, name, "")
			}
			b := &Board{ID: NewID("board"), Name: name, Canvas: Canvas{Width: clamp(w, 1, 20000), Height: clamp(h, 1, 20000), Background: colour(cmd["background"], "#ffffff")}, Layers: []*Layer{}}
			_, ci := c.P.FindBoard(cur.ID)
			at := ci + 1
			if has(cmd, "index") {
				at = int(round(num(cmd["index"], 1))) - 1
			}
			insertBoard(c.P, b, at)
			return row(c, b), nil
		},
	})

	define(Spec{
		Type: "duplicateBoard", Summary: "复制一个画板（连同全部图层），副本放在它后面并切换过去",
		Params: []Param{
			{Name: "id", Type: "string", Desc: "要复制的画板（默认当前画板）"},
			{Name: "name", Type: "string", Desc: `副本名，默认"原名 副本"`},
		},
		Returns: "board row",
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			src := c.P.ActiveBoard()
			if has(cmd, "id") {
				b, err := board(c, cmd["id"])
				if err != nil {
					return nil, err
				}
				src = b
			}
			var cp Board
			raw, _ := jsonMarshal(src)
			_ = jsonUnmarshal(raw, &cp)
			cp.ID = NewID("board")
			name := strings.TrimSpace(str(cmd["name"]))
			if name == "" {
				name = src.Name + " 副本"
			}
			cp.Name = uniqueBoard(c.P, name, "")
			_, si := c.P.FindBoard(src.ID)
			insertBoard(c.P, &cp, si+1)
			return row(c, &cp), nil
		},
	})

	define(Spec{
		Type: "removeBoard", Summary: "删除一个画板（项目至少保留一个）",
		Params: []Param{boardRef}, Returns: "{ removed, active }",
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			b, err := board(c, cmd["id"])
			if err != nil {
				return nil, err
			}
			if len(c.P.Boards) <= 1 {
				return nil, errf("a project keeps at least one board")
			}
			_, i := c.P.FindBoard(b.ID)
			c.P.Boards = append(c.P.Boards[:i], c.P.Boards[i+1:]...)
			if c.P.Active == b.ID {
				c.P.Active = c.P.Boards[int(math.Min(float64(i), float64(len(c.P.Boards)-1)))].ID
			}
			return map[string]any{"removed": b.ID, "active": c.P.Active}, nil
		},
	})

	define(Spec{
		Type: "renameBoard", Summary: "重命名画板",
		Params: []Param{boardRef, {Name: "name", Type: "string", Desc: "新名字", Required: true}}, Returns: "board row",
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			b, err := board(c, cmd["id"])
			if err != nil {
				return nil, err
			}
			name := strings.TrimSpace(str(cmd["name"]))
			if name == "" {
				return nil, errf("renameBoard needs a non-empty name")
			}
			b.Name = uniqueBoard(c.P, name, b.ID)
			return row(c, b), nil
		},
	})

	define(Spec{
		Type: "moveBoard", Summary: "调整画板顺序（第一个画板是项目的封面缩略图）",
		Params: []Param{boardRef, {Name: "index", Type: "number", Desc: "移到第几个（从 1 开始）", Required: true}}, Returns: "board row",
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			b, err := board(c, cmd["id"])
			if err != nil {
				return nil, err
			}
			_, from := c.P.FindBoard(b.ID)
			c.P.Boards = append(c.P.Boards[:from], c.P.Boards[from+1:]...)
			to := int(math.Max(0, math.Min(float64(len(c.P.Boards)), round(num(cmd["index"], 1))-1)))
			c.P.Boards = append(c.P.Boards, nil)
			copy(c.P.Boards[to+1:], c.P.Boards[to:])
			c.P.Boards[to] = b
			return row(c, b), nil
		},
	})
}
