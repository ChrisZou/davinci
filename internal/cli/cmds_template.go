package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"davinci/internal/server"
)

// `davinci tpl …` — the template library (模板库). A template is an ordinary
// project (layered, editable with every other command via -p) filed as a
// reference: an agent asked for "something like the red-banner ones I saved"
// lists templates by tag, renders one to look at it, and starts a design as a
// copy of it.

func newTemplateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tpl",
		Aliases: []string{"template", "templates"},
		Short:   "模板库：收藏的封面（完整的分层项目），作为参考",
		Long: `模板库里的模板就是普通项目（分层、可编辑、可导出），只是归在「模板库」而不是「作品」里，
用来参考和照着做。编辑模板本身和编辑作品一样，用 -p <模板id> 加任意命令。

  davinci tpl ls                              # 全部模板
  davinci tpl ls --tag 大字 -q 人物            # 按标签 + 关键词（名称/标签/备注/链接）
  davinci tpl tags                            # 用过的标签及数量
  davinci tpl show p_xxx                      # 备注、标签、来源、画板
  davinci tpl get p_xxx                       # 渲染成 PNG 存到本地（拿来看图）
  davinci tpl use p_xxx "新封面"               # 复制成一个新作品，在副本上改
  davinci tpl add cover.jpg --tags 大字 --note "标题压满上半屏"   # 把一张图收藏成模板
  davinci tpl move p_xxx                      # 把一个作品移进模板库（--to design 移回作品）`,
	}
	cmd.AddCommand(
		newTplListCmd(),
		newTplTagsCmd(),
		newTplShowCmd(),
		newTplGetCmd(),
		newTplUseCmd(),
		newTplAddCmd(),
		newTplMoveCmd(),
		newTplUpdateCmd(),
	)
	return cmd
}

func (c *Client) project(id string) (*server.Project, error) {
	var out struct {
		Project server.Project `json:"project"`
	}
	if err := c.getJSON("/api/projects/"+url.PathEscape(id), &out); err != nil {
		return nil, err
	}
	return &out.Project, nil
}

func newTplListCmd() *cobra.Command {
	var query, tag string
	cmd := &cobra.Command{
		Use:     "ls [关键词]",
		Aliases: []string{"list", "search"},
		Short:   "列出 / 搜索模板",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			q := url.Values{"kind": {server.KindTemplate}}
			if query == "" && len(args) > 0 {
				query = strings.Join(args, " ")
			}
			if query != "" {
				q.Set("q", query)
			}
			if tag != "" {
				q.Set("tag", tag)
			}
			var out struct {
				Projects []server.ProjectSummary `json:"projects"`
			}
			if err := c.getJSON("/api/projects?"+q.Encode(), &out); err != nil {
				return err
			}
			if c.JSON {
				printJSON(out)
				return nil
			}
			w := newTabWriter()
			defer w.Flush()
			fmt.Fprintln(w, "ID\t名称\t画布\t标签\t备注")
			for _, p := range out.Projects {
				fmt.Fprintf(w, "%s\t%s\t%d×%d\t%s\t%s\n", p.ID, truncate(p.Name, 28), p.Width, p.Height,
					truncate(strings.Join(p.Tags, ","), 28), truncate(strings.ReplaceAll(p.Note, "\n", " "), 36))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&query, "query", "q", "", "关键词（名称、标签、备注、链接）")
	cmd.Flags().StringVarP(&tag, "tag", "t", "", "只看带这个标签的")
	return cmd
}

func newTplTagsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tags",
		Short: "列出模板用过的标签及数量",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			var out struct {
				Tags []server.TagCount `json:"tags"`
			}
			if err := c.getJSON("/api/projects/tags?kind=template", &out); err != nil {
				return err
			}
			if c.JSON {
				printJSON(out.Tags)
				return nil
			}
			w := newTabWriter()
			defer w.Flush()
			fmt.Fprintln(w, "标签\t数量")
			for _, t := range out.Tags {
				fmt.Fprintf(w, "%s\t%d\n", t.Name, t.Count)
			}
			return nil
		},
	}
}

func newTplShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <模板 id 或名称>",
		Short: "看一个模板的备注、标签、来源和画板",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			c.Project = args[0]
			id, err := c.ProjectID()
			if err != nil {
				return err
			}
			p, err := c.project(id)
			if err != nil {
				return err
			}
			boards := server.Boards(p.Document)
			if c.JSON {
				p.Document = nil
				printJSON(map[string]any{"project": p, "boards": boards})
				return nil
			}
			kind := "模板"
			if p.Kind != server.KindTemplate {
				kind = "作品（不在模板库里）"
			}
			fmt.Printf("%s  %s\n%s，%d×%d，%d 个画板\n", p.ID, p.Name, kind, p.Width, p.Height, len(boards))
			if len(p.Tags) > 0 {
				fmt.Printf("标签：%s\n", strings.Join(p.Tags, "、"))
			}
			if p.Link != "" {
				fmt.Printf("来源：%s\n", p.Link)
			}
			if p.Note != "" {
				fmt.Printf("\n%s\n", p.Note)
			}
			fmt.Printf("\n看图：davinci tpl get %s　图层：davinci -p %s layers　照着做：davinci tpl use %s\n", p.ID, p.ID, p.ID)
			return nil
		},
	}
}

func newTplGetCmd() *cobra.Command {
	var out, board string
	var scale float64
	cmd := &cobra.Command{
		Use:   "get <模板 id 或名称>",
		Short: "把模板渲染成 PNG 存到本地，打印路径",
		Long: `把模板渲染成 PNG 存到本地文件并打印路径，方便 Agent 直接看图。
不给 -o 时存到系统临时目录；默认渲染第一个画板。`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			c.Project = args[0]
			id, err := c.ProjectID()
			if err != nil {
				return err
			}
			q := url.Values{"scale": {fmt.Sprint(scale)}, "board": {firstNonEmpty(board, "1")}}
			resp, err := c.HTTP.Get(c.BaseURL + "/api/projects/" + url.PathEscape(id) + "/export.png?" + q.Encode())
			if err != nil {
				return fmt.Errorf("渲染 %s: %w", id, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode/100 != 2 {
				raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				return apiError(raw, resp.StatusCode)
			}
			path := out
			if path == "" {
				path = filepath.Join(os.TempDir(), "davinci-"+id+".png")
			}
			f, err := os.Create(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, resp.Body); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "保存到这个路径")
	cmd.Flags().StringVar(&board, "board", "", "渲染哪个画板（默认第 1 个）")
	cmd.Flags().Float64Var(&scale, "scale", 1, "倍率")
	return cmd
}

func newTplUseCmd() *cobra.Command {
	var open bool
	cmd := &cobra.Command{
		Use:   "use <模板 id 或名称> [新作品名]",
		Short: "照着模板新建作品（复制整份分层文档）",
		Long: `把模板整份复制成一个新作品（全部画板和图层），之后在副本上改字、换图即可，模板本身不变。
新作品名默认和模板同名。`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			c.Project = args[0]
			id, err := c.ProjectID()
			if err != nil {
				return err
			}
			body := map[string]any{"kind": server.KindDesign}
			if len(args) > 1 {
				body["name"] = args[1]
			}
			var out struct {
				Project server.ProjectSummary `json:"project"`
				Editor  string                `json:"editorURL"`
			}
			if err := c.postJSON("/api/projects/"+url.PathEscape(id)+"/duplicate", body, &out); err != nil {
				return err
			}
			out.Editor = c.EditorURL(out.Project.ID)
			if c.JSON {
				printJSON(out)
				return nil
			}
			fmt.Printf("已创建作品 %s（%s，%d×%d），照着模板 %s 复制而来\n", out.Project.Name, out.Project.ID, out.Project.Width, out.Project.Height, id)
			fmt.Println(out.Editor)
			if open {
				return openEditor(c, out.Project.ID)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&open, "open", false, "创建后在浏览器里打开")
	return cmd
}

// projectFromImage posts one file (or remote URL) as a new project.
func (c *Client) projectFromImage(ref string, fields map[string]string) (*server.ProjectSummary, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if v != "" {
			_ = mw.WriteField(k, v)
		}
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		_ = mw.WriteField("ref", ref)
	} else {
		path := strings.TrimPrefix(ref, "file://")
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("读取 %s: %w", path, err)
		}
		defer f.Close()
		w, err := mw.CreateFormFile("file", filepath.Base(path))
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(w, f); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Post(c.BaseURL+"/api/projects/from-image", mw.FormDataContentType(), &buf)
	if err != nil {
		return nil, fmt.Errorf("上传 %s: %w", ref, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode/100 != 2 {
		return nil, apiError(raw, resp.StatusCode)
	}
	var out struct {
		Project server.ProjectSummary `json:"project"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out.Project, nil
}

func newTplAddCmd() *cobra.Command {
	var name, tags, note, link string
	cmd := &cobra.Command{
		Use:   "add <本地文件|URL>...",
		Short: "把图片收藏成模板（新建一个画布同尺寸、整张图做一个图层的模板项目）",
		Example: `  davinci tpl add ~/Downloads/cover.jpg --tags 大字,红底白字 --note "标题压满上半屏，人物压字"
  davinci tpl add https://example.com/a.png --link https://www.xiaohongshu.com/explore/xxx
  davinci tpl add ~/inspo/*.png --tags 极简`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			n, err := readArg(note)
			if err != nil {
				return err
			}
			if len(args) > 1 && (name != "" || link != "") {
				return fmt.Errorf("--name 和 --link 只能在添加单个文件时使用")
			}
			for _, ref := range args {
				p, err := c.projectFromImage(ref, map[string]string{"kind": server.KindTemplate, "name": name, "tags": tags, "note": n, "link": link})
				if err != nil {
					return fmt.Errorf("%s: %w", ref, err)
				}
				if c.JSON {
					printJSON(p)
					continue
				}
				fmt.Printf("已收藏 %s  %s（%d×%d）\n", p.ID, p.Name, p.Width, p.Height)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "名称（默认用文件名）")
	cmd.Flags().StringVar(&tags, "tags", "", "标签，逗号分隔")
	cmd.Flags().StringVar(&note, "note", "", "备注：喜欢它哪里，或 @文件")
	cmd.Flags().StringVar(&link, "link", "", "来源链接（原帖地址）")
	return cmd
}

func newTplMoveCmd() *cobra.Command {
	var to string
	cmd := &cobra.Command{
		Use:   "move <项目 id 或名称>...",
		Short: "把作品移进模板库（--to design 把模板移回作品）",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if to != server.KindTemplate && to != server.KindDesign {
				return fmt.Errorf("--to 只能是 template 或 design")
			}
			c, err := NewClient()
			if err != nil {
				return err
			}
			for _, ref := range args {
				c.Project = ref
				id, err := c.ProjectID()
				if err != nil {
					return err
				}
				if err := c.sendJSON(http.MethodPatch, "/api/projects/"+url.PathEscape(id), map[string]any{"kind": to}, nil); err != nil {
					return fmt.Errorf("%s: %w", ref, err)
				}
				fmt.Printf("已移到%s：%s\n", map[string]string{server.KindTemplate: "模板库", server.KindDesign: "作品"}[to], id)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&to, "to", server.KindTemplate, "template（模板库）或 design（作品）")
	return cmd
}

func newTplUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <模板 id 或名称>",
		Short: "改模板的名称、标签、备注或来源链接",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			c.Project = args[0]
			id, err := c.ProjectID()
			if err != nil {
				return err
			}
			patch := map[string]any{}
			assign(patch, "name", optStr(cmd, "name"))
			assign(patch, "link", optStr(cmd, "link"))
			if t := optStr(cmd, "tags"); t != nil {
				parts := []string{}
				for _, s := range strings.FieldsFunc(*t, func(r rune) bool { return r == ',' || r == '，' || r == '、' }) {
					parts = append(parts, strings.TrimSpace(s))
				}
				patch["tags"] = parts
			}
			if n := optStr(cmd, "note"); n != nil {
				v, err := readArg(*n)
				if err != nil {
					return err
				}
				patch["note"] = v
			}
			if len(patch) == 0 {
				return fmt.Errorf("没有要改的内容（--name / --tags / --note / --link）")
			}
			if err := c.sendJSON(http.MethodPatch, "/api/projects/"+url.PathEscape(id), patch, nil); err != nil {
				return err
			}
			fmt.Printf("已更新 %s\n", id)
			return nil
		},
	}
	cmd.Flags().String("name", "", "新名称")
	cmd.Flags().String("tags", "", "替换全部标签，逗号分隔")
	cmd.Flags().String("note", "", "新的备注，或 @文件")
	cmd.Flags().String("link", "", "来源链接")
	return cmd
}
