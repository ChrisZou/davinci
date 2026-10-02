package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"davinci/internal/server"
)

// newFontsCmd lists available fonts.
func newFontsCmd() *cobra.Command {
	var rescan, all bool
	cmd := &cobra.Command{
		Use:     "fonts",
		Aliases: []string{"font-list"},
		Short:   "列出可用字体（★ 收藏的排最前；隐藏的不列，--all 全列）",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			var out struct {
				OK    bool                `json:"ok"`
				Fonts []server.FontFamily `json:"fonts"`
				Error string              `json:"error"`
			}
			path := "/api/fonts"
			if rescan {
				path += "?rescan=1"
			}
			if err := c.getJSON(path, &out); err != nil {
				return err
			}
			if !out.OK {
				return fmt.Errorf("%s", firstNonEmpty(out.Error, "list fonts failed"))
			}
			fonts := out.Fonts[:0:0]
			for _, f := range out.Fonts {
				if all || !f.Hidden {
					fonts = append(fonts, f)
				}
			}
			if c.JSON {
				printJSON(fonts)
				return nil
			}
			w := newTabWriter()
			defer w.Flush()
			fmt.Fprintln(w, "\t字体族\t字重\t来源")
			for _, f := range fonts {
				mark := ""
				if f.Favorite {
					mark = "★"
				} else if f.Hidden {
					mark = "·"
				}
				fmt.Fprintf(w, "%s\t%s\t%v\t%s\n", mark, f.Family, f.Weights, f.Source)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&rescan, "rescan", false, "重新扫描系统字体目录")
	cmd.Flags().BoolVar(&all, "all", false, "也列出隐藏的字体（系统界面字体、其他文字的字体等）")
	return cmd
}

// newFontCmd groups font subcommands: `davinci font add <file>`.
func newFontCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "font",
		Short: "字体：add 上传 .ttf/.otf（列出字体用 davinci fonts）",
	}
	cmd.AddCommand(newFontAddCmd())
	return cmd
}

// newFontAddCmd adds a font file to the data directory.
func newFontAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <file.ttf>",
		Short: "上传一个 .ttf/.otf 字体",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			url, err := c.uploadFont(args[0])
			if err != nil {
				return err
			}
			fmt.Printf("字体已可用：%s\n", url)
			return nil
		},
	}
}

// --- document ---

func newDocCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "doc",
		Aliases: []string{"document", "show"},
		Short:   "查看 / 导入导出整份文档",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			id, err := c.ProjectID()
			if err != nil {
				return err
			}
			var out struct {
				OK      bool           `json:"ok"`
				Project server.Project `json:"project"`
			}
			if err := c.getJSON("/api/projects/"+id, &out); err != nil {
				return err
			}
			printJSON(out.Project.Document)
			return nil
		},
	}
	cmd.AddCommand(newExportDocCmd(), newImportDocCmd())
	return cmd
}

func newExportDocCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "export-doc [-o out.json]",
		Short: "把整份文档存成 JSON 文件",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			id, err := c.ProjectID()
			if err != nil {
				return err
			}
			var p struct {
				OK      bool           `json:"ok"`
				Project server.Project `json:"project"`
			}
			if err := c.getJSON("/api/projects/"+id, &p); err != nil {
				return err
			}
			b, err := json.MarshalIndent(p.Project.Document, "", "  ")
			if err != nil {
				return err
			}
			if out == "" {
				fmt.Println(string(b))
				return nil
			}
			if err := os.WriteFile(out, b, 0o644); err != nil {
				return err
			}
			fmt.Printf("已写入 %s\n", out)
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", "输出文件")
	return cmd
}

func newImportDocCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import-doc <file.json>",
		Short: "用一份文档 JSON 覆盖当前项目",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			id, err := c.ProjectID()
			if err != nil {
				return err
			}
			b, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			if err := c.putDoc(id, b); err != nil {
				return err
			}
			fmt.Printf("已导入 %s\n", args[0])
			return nil
		},
	}
}

func newRenderCmd() *cobra.Command {
	var (
		out    string
		scale  float64
		format string
		openIt bool
	)
	cmd := &cobra.Command{
		Use:     "render [-o out.png] [--scale 2] [--format jpg]",
		Aliases: []string{"export", "render-png"},
		Short:   "导出图片（由编辑器执行渲染）",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			id, err := c.ProjectID()
			if err != nil {
				return err
			}
			path := c.BaseURL + "/api/projects/" + id + "/export." + format
			q := url.Values{}
			if scale > 0 {
				q.Set("scale", strconv.FormatFloat(scale, 'f', -1, 64))
			}
			if b := strings.TrimSpace(gf.board); b != "" {
				q.Set("board", b)
			}
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			resp, err := c.HTTP.Get(path)
			if err != nil {
				return fmt.Errorf("渲染请求失败: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode/100 != 2 {
				return readAPIError(resp)
			}
			if out == "" {
				out = "davinci-" + id + "." + format
			}
			f, err := os.Create(out)
			if err != nil {
				return err
			}
			n, err := f.ReadFrom(resp.Body)
			f.Close()
			if err != nil {
				return err
			}
			fmt.Printf("已导出 %s（%s）\n", out, humanBytes(n))
			if openIt {
				openBrowser(out)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", "输出文件")
	cmd.Flags().Float64Var(&scale, "scale", 1, "导出倍率，2 就是两倍图")
	cmd.Flags().StringVar(&format, "format", "png", "png|jpg|webp")
	cmd.Flags().BoolVar(&openIt, "open", false, "导出后打开")
	return cmd
}

func newSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "打印执行器上报的命令清单（给 AI 看的工具表）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			body, err := c.getRaw("/api/schema")
			if err != nil {
				return err
			}
			fmt.Println(string(body))
			return nil
		},
	}
}

// newExecCmd runs a raw command object, the universal fallback: any capability
// exists as a command before it exists as a convenience subcommand.
func newExecCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:     "exec '<JSON>'",
		Aliases: []string{"raw"},
		Short:   "执行一条原始命令 JSON",
		Example: `  davinci exec '{"type":"addText","text":"你好","x":100,"y":200}'
  davinci exec '[{"type":"addShape","kind":"rect","width":100,"height":100,"fill":"#f00"}]'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			raw := ""
			if file != "" {
				b, err := os.ReadFile(file)
				if err != nil {
					return err
				}
				raw = string(b)
			} else if len(args) == 1 {
				raw = args[0]
			} else {
				return fmt.Errorf("给一段命令 JSON，或用 --file")
			}
			var cmds []map[string]any
			trimmed := trimSpace(raw)
			if len(trimmed) > 0 && trimmed[0] == '[' {
				if err := json.Unmarshal([]byte(trimmed), &cmds); err != nil {
					return fmt.Errorf("JSON 解析失败: %w", err)
				}
			} else {
				var one map[string]any
				if err := json.Unmarshal([]byte(trimmed), &one); err != nil {
					return fmt.Errorf("JSON 解析失败: %w", err)
				}
				cmds = []map[string]any{one}
			}
			if len(cmds) == 0 {
				return fmt.Errorf("命令列表是空的")
			}
			return c.execAndPrint(cmds...)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "从文件读取命令 JSON")
	return cmd
}

func newUndoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "undo",
		Short: "撤销一步（和历史栈里人的操作共用一个栈）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			return c.execAndPrint(map[string]any{"type": "undo"})
		},
	}
}

func newRedoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "redo",
		Short: "重做一步",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			return c.execAndPrint(map[string]any{"type": "redo"})
		},
	}
}
