package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// joinArgs joins positional arguments with a space, so text with spaces needs no
// quoting gymnastics.
func joinArgs(args []string) string { return strings.Join(args, " ") }

// parseFloat keeps error messages in the CLI's own voice.
func parseFloat(s string) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("不是数字：%s", s)
	}
	return f, nil
}

// clientAndLayer makes a client and resolves a layer reference in one step.
func clientAndLayer(ref string) (*Client, *layerRef, error) {
	c, err := NewClient()
	if err != nil {
		return nil, nil, err
	}
	l, err := c.ResolveLayer(ref)
	if err != nil {
		return nil, nil, err
	}
	return c, &layerRef{id: l.ID, name: l.Name, kind: l.Type}, nil
}

// layerRef is the resolved identity of a layer, with everything a command needs.
type layerRef struct {
	id   string
	name string
	kind string
}

func newTextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "text",
		Aliases: []string{"T"},
		Short:   "文字图层操作",
	}
	cmd.AddCommand(newTextSetCmd(), newTextStyleCmd(), newTextPresetCmd(), newTextPresetsCmd())
	return cmd
}

func newTextSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "set <id|名称> <新文字>",
		Aliases: []string{"content"},
		Short:   "改文字内容",
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			return c.execAndPrint(map[string]any{"type": "setText", "id": l.id, "text": joinArgs(args[1:])})
		},
	}
}

func newTextStyleCmd() *cobra.Command {
	var (
		size    float64
		color   string
		font    string
		weight  int
		italic  bool
		bg      string
		shadow  string
		stroke  string
		align   string
		lineH   float64
		ls      float64
		opacity float64
	)
	cmd := &cobra.Command{
		Use:     "style <id|名称>",
		Aliases: []string{"css"},
		Short:   "改文字样式",
		Example: `  davinci text style 标题 --size 120 --color '#fff' --bg '#e5322d' --shadow 'rgba(0,0,0,.6):18:4:4' --stroke '#000:6'`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			style := map[string]any{}
			assign(style, "fontSize", optFloat(cmd, "size"))
			assign(style, "fill", optStr(cmd, "color"))
			assign(style, "fontFamily", optStr(cmd, "font"))
			assign(style, "fontWeight", optInt(cmd, "weight"))
			assign(style, "fontStyle", optBool(cmd, "italic"))
			assign(style, "textBackgroundColor", optStr(cmd, "bg"))
			assign(style, "shadow", optStr(cmd, "shadow"))
			assign(style, "stroke", optStr(cmd, "stroke"))
			assign(style, "textAlign", optStr(cmd, "align"))
			assign(style, "lineHeight", optFloat(cmd, "line-height"))
			assign(style, "charSpacing", optFloat(cmd, "letter-spacing"))
			assign(style, "opacity", optFloat(cmd, "opacity"))
			if len(style) == 0 {
				return fmt.Errorf("至少给一个样式参数，用 --help 看有哪些")
			}
			return c.execAndPrint(map[string]any{"type": "setTextStyle", "id": l.id, "style": style})
		},
	}
	cmd.Flags().Float64Var(&size, "size", 0, "字号")
	cmd.Flags().StringVar(&color, "color", "", "文字颜色")
	cmd.Flags().StringVar(&font, "font", "", "字体族名")
	cmd.Flags().IntVar(&weight, "weight", 0, "字重 100-900")
	cmd.Flags().BoolVar(&italic, "italic", false, "斜体")
	cmd.Flags().StringVar(&bg, "bg", "", "文字背景色")
	cmd.Flags().StringVar(&shadow, "shadow", "", "阴影：颜色:模糊:偏移x:偏移y")
	cmd.Flags().StringVar(&stroke, "stroke", "", "描边：颜色[:粗细]")
	cmd.Flags().StringVar(&align, "align", "", "对齐 left|center|right")
	cmd.Flags().Float64Var(&lineH, "line-height", 0, "行高倍数")
	cmd.Flags().Float64Var(&ls, "letter-spacing", 0, "字距")
	cmd.Flags().Float64Var(&opacity, "opacity", 0, "不透明度 0-1")
	return cmd
}

func newTextPresetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "preset <id|名称> <预设名>",
		Short: "套用封面常用文字预设（用 `davinci text presets` 看列表）",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			return c.execAndPrint(map[string]any{"type": "applyTextPreset", "id": l.id, "preset": args[1]})
		},
	}
}

// newTextPresetsCmd lists the cover-style text presets. `text preset` needs the
// keys to be discoverable: guessing one is a wasted round trip, and the preset
// names are the kind of thing a person forgets between uses.
func newTextPresetsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "presets",
		Aliases: []string{"preset-list"},
		Short:   "列出封面文字预设",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			res, err := c.Exec(map[string]any{"type": "listTextPresets"})
			if err != nil {
				return err
			}
			var out struct {
				Presets []struct {
					Key   string         `json:"key"`
					Name  string         `json:"name"`
					Style map[string]any `json:"style"`
				} `json:"presets"`
			}
			if err := json.Unmarshal(res.Data, &out); err != nil {
				return err
			}
			if c.JSON {
				printJSON(out.Presets)
				return nil
			}
			w := newTabWriter()
			defer w.Flush()
			fmt.Fprintln(w, "预设\t说明\t样式")
			for _, p := range out.Presets {
				fmt.Fprintf(w, "%s\t%s\t%s\n", p.Key, p.Name, oneLineJSON(p.Style))
			}
			return nil
		},
	}
}

func newImageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "image",
		Aliases: []string{"img"},
		Short:   "图片图层操作",
	}
	cmd.AddCommand(newImageSetCmd(), newImageReplaceCmd(), newImageCutoutCmd())
	return cmd
}

func newImageSetCmd() *cobra.Command {
	var (
		radius    float64
		blur      float64
		bright    float64
		contrast  float64
		saturate  float64
		grayscale bool
		flipX     bool
		flipY     bool
		opacity   float64
	)
	cmd := &cobra.Command{
		Use:     "set <id|名称>",
		Aliases: []string{"style"},
		Short:   "改图片样式（圆角、滤镜、翻转）",
		Example: `  davinci image set 人像 --radius 20 --blur 3 --grayscale`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			props := map[string]any{}
			assign(props, "cornerRadius", optFloat(cmd, "radius"))
			assign(props, "opacity", optFloat(cmd, "opacity"))
			if cmd.Flags().Changed("flip-x") {
				props["flipX"] = flipX
			}
			if cmd.Flags().Changed("flip-y") {
				props["flipY"] = flipY
			}
			filters := map[string]any{}
			assign(filters, "blur", optFloat(cmd, "blur"))
			assign(filters, "brightness", optFloat(cmd, "brightness"))
			assign(filters, "contrast", optFloat(cmd, "contrast"))
			assign(filters, "saturation", optFloat(cmd, "saturation"))
			if cmd.Flags().Changed("grayscale") {
				filters["grayscale"] = grayscale
			}
			if len(filters) > 0 {
				props["filters"] = filters
			}
			if len(props) == 0 {
				return fmt.Errorf("至少给一个参数，用 --help 看有哪些")
			}
			return c.execAndPrint(map[string]any{"type": "setImageProps", "id": l.id, "props": props})
		},
	}
	cmd.Flags().Float64Var(&radius, "radius", 0, "圆角半径")
	cmd.Flags().Float64Var(&blur, "blur", 0, "高斯模糊 0-1")
	cmd.Flags().Float64Var(&bright, "brightness", 0, "亮度 -1..1")
	cmd.Flags().Float64Var(&contrast, "contrast", 0, "对比度 -1..1")
	cmd.Flags().Float64Var(&saturate, "saturation", 0, "饱和度 -1..1")
	cmd.Flags().BoolVar(&grayscale, "grayscale", false, "黑白")
	cmd.Flags().BoolVar(&flipX, "flip-x", false, "水平翻转")
	cmd.Flags().BoolVar(&flipY, "flip-y", false, "垂直翻转")
	cmd.Flags().Float64Var(&opacity, "opacity", 0, "不透明度 0-1")
	return cmd
}

func newImageReplaceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "replace <id|名称> <本地路径|URL>",
		Short: "换掉图层里的图片（保留尺寸和位置）",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			url, err := c.UploadAsset(args[1])
			if err != nil {
				return err
			}
			return c.execAndPrint(map[string]any{"type": "replaceImage", "id": l.id, "url": url})
		},
	}
}

func newImageCutoutCmd() *cobra.Command {
	var portrait, restore bool
	cmd := &cobra.Command{
		Use:     "rmbg <id|名称>",
		Aliases: []string{"cutout", "rembg"},
		Short:   "去除图片背景（rembg + BiRefNet；--portrait 人像模型，--restore 恢复原图）",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			m := map[string]any{"type": "removeBackground", "id": l.id}
			if restore {
				m["restore"] = true
			} else if portrait {
				m["model"] = "portrait"
			}
			return c.execAndPrint(m)
		},
	}
	cmd.Flags().BoolVar(&portrait, "portrait", false, "用人像模型（头发边缘更细）")
	cmd.Flags().BoolVar(&restore, "restore", false, "换回去背景之前的原图")
	return cmd
}

func newCanvasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "canvas",
		Aliases: []string{"c"},
		Short:   "画布操作",
	}
	cmd.AddCommand(newCanvasSizeCmd(), newCanvasBgCmd(), newCanvasPresetsCmd())
	return cmd
}

func newCanvasSizeCmd() *cobra.Command {
	var w, h int
	cmd := &cobra.Command{
		Use:   "size --w 1242 --h 1656",
		Short: "改画布尺寸（图层不动）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			return c.execAndPrint(map[string]any{"type": "setCanvasSize", "width": w, "height": h})
		},
	}
	cmd.Flags().IntVar(&w, "w", 0, "宽")
	cmd.Flags().IntVar(&h, "h", 0, "高")
	cmd.MarkFlagRequired("w")
	cmd.MarkFlagRequired("h")
	return cmd
}

func newCanvasBgCmd() *cobra.Command {
	var (
		color string
		image string
	)
	cmd := &cobra.Command{
		Use:   "bg",
		Short: "改画布背景",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			m := map[string]any{"type": "setBackground"}
			if image != "" {
				url, err := c.UploadAsset(image)
				if err != nil {
					return err
				}
				m["image"] = url
			} else if color != "" {
				m["color"] = color
			} else {
				return fmt.Errorf("给 --color 或 --image 之一")
			}
			return c.execAndPrint(m)
		},
	}
	cmd.Flags().StringVar(&color, "color", "", "背景色，如 #ffffff")
	cmd.Flags().StringVar(&image, "image", "", "背景图片（本地路径或 URL）")
	return cmd
}

// newCanvasPresetsCmd lists the canvas presets, so `canvas size` and
// `setCanvasSize preset:` have visible keys instead of folklore.
func newCanvasPresetsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "presets",
		Aliases: []string{"preset-list"},
		Short:   "列出画布尺寸预设",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			res, err := c.Exec(map[string]any{"type": "listCanvasPresets"})
			if err != nil {
				return err
			}
			var out struct {
				Presets []struct {
					Key    string `json:"key"`
					Name   string `json:"name"`
					Width  int    `json:"width"`
					Height int    `json:"height"`
				} `json:"presets"`
			}
			if err := json.Unmarshal(res.Data, &out); err != nil {
				return err
			}
			if c.JSON {
				printJSON(out.Presets)
				return nil
			}
			w := newTabWriter()
			defer w.Flush()
			fmt.Fprintln(w, "预设\t说明\t尺寸")
			for _, p := range out.Presets {
				fmt.Fprintf(w, "%s\t%s\t%d×%d\n", p.Key, p.Name, p.Width, p.Height)
			}
			return nil
		},
	}
}
