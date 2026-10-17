package doc

import (
	"fmt"
	"regexp"
	"strings"
)

// PaletteColors has exactly the five semantic roles shared by storage and commands.
type PaletteColors struct {
	BG        string `json:"bg"`
	Text      string `json:"text"`
	Highlight string `json:"highlight"`
	Text2     string `json:"text2"`
	Accent    string `json:"accent"`
}

var paletteHex = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (p *PaletteColors) Normalize() error {
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"bg", &p.BG}, {"text", &p.Text}, {"highlight", &p.Highlight}, {"text2", &p.Text2}, {"accent", &p.Accent},
	} {
		v := strings.TrimSpace(*field.value)
		if !paletteHex.MatchString(v) {
			return fmt.Errorf("%s 必须是 #RRGGBB 色值", field.name)
		}
		*field.value = strings.ToLower(v)
	}
	return nil
}

// Optional service: document-only engines can still run all other commands.
type paletteSource interface {
	PaletteColors(id string) (PaletteColors, error)
}

func init() {
	define(Spec{
		Type: "applyPalette", Summary: "应用已保存配色到画板（含编组内图层），一次撤销恢复全部",
		Params: []Param{
			{Name: "id", Type: "string", Required: true, Desc: "全局配色 id（davinci palette list）"},
			{Name: "board", Type: "string", Desc: "画板 id、名称或序号，默认当前画板"},
		},
		Run: func(c *Ctx, cmd map[string]any) (any, error) {
			source, ok := c.svc().(paletteSource)
			if !ok {
				return nil, errf("配色服务不可用")
			}
			colors, err := source.PaletteColors(str(cmd["id"]))
			if err != nil {
				return nil, err
			}
			if err := colors.Normalize(); err != nil {
				return nil, err
			}
			b := c.B
			if ref := str(cmd["board"]); ref != "" {
				b, _ = c.P.FindBoard(ref)
				if b == nil {
					return nil, errf("找不到画板 %q", ref)
				}
			}
			// Resolve inside the command so both failure and undo restore the active board.
			c.P.Active = b.ID
			b.Canvas.Background = colors.BG
			var layers []*Layer
			var walk func([]*Layer)
			walk = func(ls []*Layer) {
				for _, l := range ls {
					if l.Type == "group" {
						walk(l.Children)
					} else {
						layers = append(layers, l)
					}
				}
			}
			walk(b.Layers)
			largest := 0.0
			for _, l := range layers {
				if l.Type == "text" {
					largest = max(largest, num(l.S("fontSize"), 40))
				}
			}
			shapeIndex := 0
			for _, l := range layers {
				switch l.Type {
				case "text":
					fill := colors.Text2
					name := strings.ToLower(strings.TrimSpace(l.Name))
					if num(l.S("fontSize"), 40) == largest || name == "标题" || name == "主标题" || name == "title" || name == "headline" {
						fill = colors.Text
					}
					l.SetS("fill", fill)
				case "shape":
					// A full-board unrotated rectangle at the root is a background fill.
					background := false
					for _, root := range b.Layers {
						if root == l && (l.Shape == nil || l.Shape.Kind == "rect") && l.Rotation == 0 && l.X <= 0 && l.Y <= 0 && l.X+l.Width >= b.Canvas.Width && l.Y+l.Height >= b.Canvas.Height {
							background = true
							break
						}
					}
					if background {
						l.SetS("fill", colors.BG)
						continue
					}
					fill, stroke := colors.Highlight, colors.Accent
					if shapeIndex%2 == 1 {
						fill, stroke = stroke, fill
					}
					shapeIndex++
					if l.Shape == nil || l.Shape.Kind != "line" {
						if f := str(l.S("fill")); f != "none" && f != "transparent" && f != "" {
							l.SetS("fill", fill)
						}
					}
					if s := str(l.S("stroke")); s != "" && s != "none" && s != "transparent" {
						// Legacy documents may embed width in stroke; keep it intact.
						if i := strings.LastIndex(s, ":"); i >= 0 {
							stroke += s[i:]
						}
						l.SetS("stroke", stroke)
					}
				}
			}
			return map[string]any{"id": str(cmd["id"]), "board": b.ID, "message": "已应用配色"}, nil
		},
	})
}
