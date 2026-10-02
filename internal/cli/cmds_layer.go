package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newLayerCmd is the `davinci layer ...` group.
func newLayerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "layer",
		Aliases: []string{"L"},
		Short:   "图层操作（增删改、层级、对齐）",
	}
	cmd.AddCommand(
		newLayerAddTextCmd(),
		newLayerAddImageCmd(),
		newLayerAddShapeCmd(),
		newLayerShowCmd(),
		newLayerMoveCmd(),
		newLayerSizeCmd(),
		newLayerRotateCmd(),
		newLayerRemoveCmd(),
		newLayerDuplicateCmd(),
		newLayerRenameCmd(),
		newLayerZCmd(),
		newLayerAlignCmd(),
		newLayerOrderCmd(),
		newLayerUpdateCmd(),
		newLayerVisibleCmd(),
		newLayerLockCmd(),
		newLayerFitCmd(),
	)
	return cmd
}

// newLayersCmd lists layers.
func newLayersCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "layers",
		Aliases: []string{"list"},
		Short:   "列出图层（从最上层到最底层）",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			list, err := c.Layers()
			if err != nil {
				return err
			}
			c.printLayers(list)
			return nil
		},
	}
}

func newLayerShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "show <id|名称>",
		Aliases: []string{"get", "info"},
		Short:   "查看单个图层",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			l, err := c.ResolveLayer(args[0])
			if err != nil {
				return err
			}
			c.printLayer(*l)
			return nil
		},
	}
}

func newLayerAddTextCmd() *cobra.Command {
	var (
		x, y, w float64
		size    float64
		color   string
		font    string
		weight  int
		bg      string
		stroke  string
		shadow  string
		align   string
		lineH   float64
		ls      float64
		name    string
	)
	cmd := &cobra.Command{
		Use:     "add-text <文字内容>",
		Aliases: []string{"text"},
		Short:   "添加文字图层",
		Example: `  davinci layer add-text "AI 编程实测" --x 80 --y 300 --size 160 --color '#fff' --bg '#e5322d'
  davinci layer add-text "标题" --size 160 --stroke '#000:8' --shadow 'rgba(0,0,0,.6):18:4:4'`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			style := map[string]any{}
			assign(style, "fontSize", optFloat(cmd, "size"))
			assign(style, "fill", optStr(cmd, "color"))
			assign(style, "fontFamily", optStr(cmd, "font"))
			assign(style, "fontWeight", optInt(cmd, "weight"))
			assign(style, "textBackgroundColor", optStr(cmd, "bg"))
			assign(style, "stroke", optStr(cmd, "stroke"))
			assign(style, "shadow", optStr(cmd, "shadow"))
			assign(style, "textAlign", optStr(cmd, "align"))
			assign(style, "lineHeight", optFloat(cmd, "line-height"))
			assign(style, "charSpacing", optFloat(cmd, "letter-spacing"))
			m := map[string]any{"type": "addText", "text": joinArgs(args)}
			assign(m, "x", optFloat(cmd, "x"))
			assign(m, "y", optFloat(cmd, "y"))
			assign(m, "width", optFloat(cmd, "w"))
			assign(m, "name", optStr(cmd, "name"))
			if len(style) > 0 {
				m["style"] = style
			}
			return c.execAndPrint(m)
		},
	}
	cmd.Flags().Float64Var(&x, "x", 0, "左边距（画布坐标）")
	cmd.Flags().Float64Var(&y, "y", 0, "上边距（画布坐标）")
	cmd.Flags().Float64Var(&w, "w", 0, "文本框宽度（文字按此折行）")
	cmd.Flags().Float64Var(&size, "size", 0, "字号")
	cmd.Flags().StringVar(&color, "color", "", "文字颜色，如 #ffffff")
	cmd.Flags().StringVar(&font, "font", "", "字体族名，用 `davinci fonts` 查看")
	cmd.Flags().IntVar(&weight, "weight", 0, "字重 100-900")
	cmd.Flags().StringVar(&bg, "bg", "", "文字背景色")
	cmd.Flags().StringVar(&stroke, "stroke", "", "描边：颜色[:粗细]，如 '#000:8'")
	cmd.Flags().StringVar(&shadow, "shadow", "", "阴影：颜色:模糊:偏移x:偏移y")
	cmd.Flags().StringVar(&align, "align", "", "对齐 left|center|right")
	cmd.Flags().Float64Var(&lineH, "line-height", 0, "行高倍数")
	cmd.Flags().Float64Var(&ls, "letter-spacing", 0, "字距")
	cmd.Flags().StringVar(&name, "name", "", "图层名称（默认用文字内容）")
	return cmd
}

func newLayerAddImageCmd() *cobra.Command {
	var (
		x, y, w, h float64
		name       string
	)
	cmd := &cobra.Command{
		Use:     "add-image <本地路径|URL>",
		Aliases: []string{"img", "image"},
		Short:   "添加图片图层（本机文件或 http(s) 链接）",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			url, err := c.UploadAsset(args[0])
			if err != nil {
				return err
			}
			m := map[string]any{"type": "addImage", "url": url}
			assign(m, "x", optFloat(cmd, "x"))
			assign(m, "y", optFloat(cmd, "y"))
			assign(m, "width", optFloat(cmd, "w"))
			assign(m, "height", optFloat(cmd, "h"))
			assign(m, "name", optStr(cmd, "name"))
			return c.execAndPrint(m)
		},
	}
	cmd.Flags().Float64Var(&x, "x", 0, "左边距")
	cmd.Flags().Float64Var(&y, "y", 0, "上边距")
	cmd.Flags().Float64Var(&w, "w", 0, "宽度（默认按原始尺寸）")
	cmd.Flags().Float64Var(&h, "h", 0, "高度（默认等比）")
	cmd.Flags().StringVar(&name, "name", "", "图层名称")
	return cmd
}

func newLayerAddShapeCmd() *cobra.Command {
	var (
		x, y, w, h float64
		fill       string
		stroke     string
		strokeW    float64
		radius     float64
		opacity    float64
		name       string
	)
	cmd := &cobra.Command{
		Use:     "add-shape <rect|ellipse|triangle>",
		Aliases: []string{"shape"},
		Short:   "添加形状图层",
		Example: `  davinci layer add-shape rect --x 0 --y 1400 --w 1242 --h 256 --fill '#e5322d' --opacity .8
  davinci layer add-shape rect --x 96 --y 900 --w 900 --h 4 --fill '#ffffff'   # 分割线`, // 细矩形就是分割线
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			m := map[string]any{"type": "addShape", "kind": args[0]}
			assign(m, "x", optFloat(cmd, "x"))
			assign(m, "y", optFloat(cmd, "y"))
			assign(m, "width", optFloat(cmd, "w"))
			assign(m, "height", optFloat(cmd, "h"))
			assign(m, "fill", optStr(cmd, "fill"))
			assign(m, "stroke", optStr(cmd, "stroke"))
			assign(m, "strokeWidth", optFloat(cmd, "stroke-width"))
			assign(m, "cornerRadius", optFloat(cmd, "radius"))
			assign(m, "opacity", optFloat(cmd, "opacity"))
			assign(m, "name", optStr(cmd, "name"))
			return c.execAndPrint(m)
		},
	}
	cmd.Flags().Float64Var(&x, "x", 0, "左边距")
	cmd.Flags().Float64Var(&y, "y", 0, "上边距")
	cmd.Flags().Float64Var(&w, "w", 100, "宽度")
	cmd.Flags().Float64Var(&h, "h", 100, "高度")
	cmd.Flags().StringVar(&fill, "fill", "#000000", "填充色")
	cmd.Flags().StringVar(&stroke, "stroke", "", "描边色")
	cmd.Flags().Float64Var(&strokeW, "stroke-width", 0, "描边粗细")
	cmd.Flags().Float64Var(&radius, "radius", 0, "圆角")
	cmd.Flags().Float64Var(&opacity, "opacity", 1, "不透明度 0-1")
	cmd.Flags().StringVar(&name, "name", "", "图层名称")
	return cmd
}

func newLayerMoveCmd() *cobra.Command {
	var x, y float64
	cmd := &cobra.Command{
		Use:     "mv <id|名称> --x 100 --y 200",
		Aliases: []string{"move"},
		Short:   "移动图层（绝对坐标）",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			return c.execAndPrint(map[string]any{"type": "moveLayer", "id": l.id, "x": x, "y": y})
		},
	}
	cmd.Flags().Float64Var(&x, "x", 0, "新左边距")
	cmd.Flags().Float64Var(&y, "y", 0, "新上边距")
	cmd.MarkFlagRequired("x")
	cmd.MarkFlagRequired("y")
	return cmd
}

func newLayerSizeCmd() *cobra.Command {
	var w, h float64
	cmd := &cobra.Command{
		Use:     "size <id|名称> --w 800 [--h 600]",
		Aliases: []string{"resize", "scale"},
		Short:   "改图层尺寸（只给 --w 时等比缩放）",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			m := map[string]any{"type": "resizeLayer", "id": l.id}
			assign(m, "width", optFloat(cmd, "w"))
			assign(m, "height", optFloat(cmd, "h"))
			return c.execAndPrint(m)
		},
	}
	cmd.Flags().Float64Var(&w, "w", 0, "新宽度")
	cmd.Flags().Float64Var(&h, "h", 0, "新高度")
	return cmd
}

func newLayerRotateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "rotate <id|名称> <度数>",
		Aliases: []string{"rot"},
		Short:   "旋转图层（度数可以是负数，如 -12）",
		// A negative angle must not be read as a flag, so flags stop being
		// parsed at the first argument: `rotate 标题 -12` just works, while
		// `--project`/`--server` still go before the layer name.
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			deg, err := parseFloat(args[1])
			if err != nil {
				return fmt.Errorf("度数要是个数字：%s", args[1])
			}
			return c.execAndPrint(map[string]any{"type": "rotateLayer", "id": l.id, "rotation": deg})
		},
	}
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func newLayerRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rm <id|名称>",
		Aliases: []string{"delete", "del"},
		Short:   "删除图层",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			return c.execAndPrint(map[string]any{"type": "removeLayer", "id": l.id})
		},
	}
}

func newLayerDuplicateCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "cp <id|名称>",
		Aliases: []string{"duplicate", "dup"},
		Short:   "复制图层",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			return c.execAndPrint(map[string]any{"type": "duplicateLayer", "id": l.id})
		},
	}
}

func newLayerRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <id|名称> <新名称>",
		Short: "重命名图层",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			return c.execAndPrint(map[string]any{"type": "renameLayer", "id": l.id, "name": args[1]})
		},
	}
}

// newLayerZCmd changes a layer's stacking position: front | back | up | down | <n>.
func newLayerZCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "z <id|名称> <front|back|up|down|数字>",
		Aliases: []string{"zindex"},
		Short:   "调整图层上下级关系",
		Long: `调整图层的层级（z-index）：
  front / back   置顶 / 置底
  up / down      上移 / 下移一层
  数字            直接设成第几层（0 是最底层）`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			switch args[1] {
			case "front", "top":
				return c.execAndPrint(map[string]any{"type": "bringToFront", "id": l.id})
			case "back", "bottom":
				return c.execAndPrint(map[string]any{"type": "sendToBack", "id": l.id})
			case "up":
				return c.execAndPrint(map[string]any{"type": "bringForward", "id": l.id})
			case "down":
				return c.execAndPrint(map[string]any{"type": "sendBackward", "id": l.id})
			}
			n, err := parseFloat(args[1])
			if err != nil {
				return fmt.Errorf("层级可以是 front|back|up|down 或一个数字，收到：%s", args[1])
			}
			return c.execAndPrint(map[string]any{"type": "setZIndex", "id": l.id, "index": int(n)})
		},
	}
}

func newLayerAlignCmd() *cobra.Command {
	var h, v string
	cmd := &cobra.Command{
		Use:   "align <id|名称> --h center --v middle",
		Short: "把图层对齐到画布",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			m := map[string]any{"type": "alignLayer", "id": l.id}
			assign(m, "h", optStr(cmd, "h"))
			assign(m, "v", optStr(cmd, "v"))
			return c.execAndPrint(m)
		},
	}
	cmd.Flags().StringVar(&h, "h", "", "水平：left|center|right")
	cmd.Flags().StringVar(&v, "v", "", "垂直：top|middle|bottom")
	return cmd
}

// newLayerOrderCmd reorders layers by listing them in the target order.
func newLayerOrderCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "order <id|名称>...",
		Aliases: []string{"reorder"},
		Short:   "按给出的顺序重排图层（第一个在最底层）",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			ids := []string{}
			for _, ref := range args {
				l, err := c.ResolveLayer(ref)
				if err != nil {
					return err
				}
				ids = append(ids, l.ID)
			}
			return c.execAndPrint(map[string]any{"type": "reorderLayers", "ids": ids})
		},
	}
}

func newLayerUpdateCmd() *cobra.Command {
	var (
		x, y, w, h float64
		opacity    float64
		rotation   float64
		name       string
	)
	cmd := &cobra.Command{
		Use:     "update <id|名称>",
		Aliases: []string{"set"},
		Short:   "直接改图层的通用属性",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			props := map[string]any{}
			assign(props, "x", optFloat(cmd, "x"))
			assign(props, "y", optFloat(cmd, "y"))
			assign(props, "width", optFloat(cmd, "w"))
			assign(props, "height", optFloat(cmd, "h"))
			assign(props, "rotation", optFloat(cmd, "rotate"))
			assign(props, "opacity", optFloat(cmd, "opacity"))
			assign(props, "name", optStr(cmd, "name"))
			return c.execAndPrint(map[string]any{"type": "updateLayer", "id": l.id, "props": props})
		},
	}
	cmd.Flags().Float64Var(&x, "x", 0, "")
	cmd.Flags().Float64Var(&y, "y", 0, "")
	cmd.Flags().Float64Var(&w, "w", 0, "")
	cmd.Flags().Float64Var(&h, "h", 0, "")
	cmd.Flags().Float64Var(&rotation, "rotate", 0, "旋转角度")
	cmd.Flags().Float64Var(&opacity, "opacity", 0, "不透明度 0-1")
	cmd.Flags().StringVar(&name, "name", "", "图层名称")
	return cmd
}

// hiding and unhiding are one command with two names: the direction comes from
// the name the user actually typed, so `davinci layer hide 标题` needs no flag.
// Neither name is "show" — that one means "tell me about this layer" up above.
func newLayerVisibleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "hide <id|名称>",
		Aliases: []string{"unhide"},
		Short:   "隐藏 / 恢复显示图层",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			return c.execAndPrint(map[string]any{
				"type":    "setVisible",
				"id":      l.id,
				"visible": cmd.CalledAs() != "hide",
			})
		},
	}
	return cmd
}

func newLayerLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "lock <id|名称>",
		Aliases: []string{"unlock"},
		Short:   "锁定 / 解锁图层（锁定后不能拖动）",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			return c.execAndPrint(map[string]any{
				"type":   "setLocked",
				"id":     l.id,
				"locked": cmd.CalledAs() != "unlock",
			})
		},
	}
	return cmd
}

func newLayerFitCmd() *cobra.Command {
	var mode string
	cmd := &cobra.Command{
		Use:     "fit <id|名称> [cover|contain]",
		Aliases: []string{"fill"},
		Short:   "让图片铺满或装入画布",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, l, err := clientAndLayer(args[0])
			if err != nil {
				return err
			}
			m := mode
			if len(args) == 2 {
				m = args[1]
			}
			if m != "cover" && m != "contain" {
				return fmt.Errorf("模式只能是 cover 或 contain，收到 %q", m)
			}
			return c.execAndPrint(map[string]any{"type": "fitToCanvas", "id": l.id, "mode": m})
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "cover", "cover 铺满（裁切）| contain 装入（留边）")
	return cmd
}
