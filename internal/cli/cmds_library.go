package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"davinci/internal/server"
)

// `davinci lib …` — the material library from the command line. This is how an
// agent finds "a cut-out of 创哥 pointing, looking surprised" and drops it into
// the design: `lib ls -q 指向` to search, `lib show` to read the annotation,
// `lib insert` to place it.

func newLibraryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "lib",
		Aliases: []string{"library"},
		Short:   "素材库：人像、背景、APP Logo 等可复用素材",
		Long: `素材库里的素材按分类存放，所有项目共用。

  davinci lib cats                          # 分类及数量
  davinci lib ls --category 人像 -q 指向     # 按分类 + 关键词找（名称/标签/说明都会搜）
  davinci lib show lib_xxx                  # 看一条素材的完整说明（含标注 JSON）
  davinci lib insert lib_xxx --y 600        # 插入到当前项目（默认等比缩进画布）
  davinci lib add ~/Pictures/logo.png --category "APP Logo" --tags 飞书,办公`,
	}
	cmd.AddCommand(
		newLibCatsCmd(),
		newLibCategoryCmd(),
		newLibListCmd(),
		newLibShowCmd(),
		newLibAddCmd(),
		newLibUpdateCmd(),
		newLibRemoveCmd(),
		newLibInsertCmd(),
	)
	return cmd
}

func (c *Client) libCategories() ([]server.LibraryCategory, error) {
	var out struct {
		Categories []server.LibraryCategory `json:"categories"`
	}
	if err := c.getJSON("/api/library/categories", &out); err != nil {
		return nil, err
	}
	return out.Categories, nil
}

func (c *Client) libItem(id string) (*server.LibraryItem, error) {
	var out struct {
		Item server.LibraryItem `json:"item"`
	}
	if err := c.getJSON("/api/library/items/"+url.PathEscape(id), &out); err != nil {
		return nil, err
	}
	return &out.Item, nil
}

// sendJSON issues a PATCH (or any method) with a JSON body.
func (c *Client) sendJSON(method, path string, payload, out any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, c.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("请求 %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode/100 != 2 {
		return apiError(raw, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func newLibCatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "cats",
		Aliases: []string{"categories"},
		Short:   "列出分类",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			cats, err := c.libCategories()
			if err != nil {
				return err
			}
			if c.JSON {
				printJSON(cats)
				return nil
			}
			w := newTabWriter()
			defer w.Flush()
			fmt.Fprintln(w, "ID\t分类\t数量")
			for _, cat := range cats {
				fmt.Fprintf(w, "%s\t%s\t%d\n", cat.ID, cat.Name, cat.Count)
			}
			return nil
		},
	}
}

func newLibCategoryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "category", Aliases: []string{"cat"}, Short: "新建 / 重命名 / 删除分类"}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "add <名称>",
			Short: "新建分类",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := NewClient()
				if err != nil {
					return err
				}
				var out struct {
					Category server.LibraryCategory `json:"category"`
				}
				if err := c.postJSON("/api/library/categories", map[string]any{"name": args[0]}, &out); err != nil {
					return err
				}
				fmt.Printf("已新建分类：%s（%s）\n", out.Category.Name, out.Category.ID)
				return nil
			},
		},
		&cobra.Command{
			Use:   "rename <分类> <新名称>",
			Short: "重命名分类",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := NewClient()
				if err != nil {
					return err
				}
				if err := c.sendJSON(http.MethodPatch, "/api/library/categories/"+url.PathEscape(args[0]), map[string]any{"name": args[1]}, nil); err != nil {
					return err
				}
				fmt.Printf("已重命名为：%s\n", args[1])
				return nil
			},
		},
		&cobra.Command{
			Use:   "rm <分类>",
			Short: "删除分类（必须先清空）",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				c, err := NewClient()
				if err != nil {
					return err
				}
				if err := c.delete("/api/library/categories/" + url.PathEscape(args[0])); err != nil {
					return err
				}
				fmt.Printf("已删除分类：%s\n", args[0])
				return nil
			},
		},
	)
	return cmd
}

func newLibListCmd() *cobra.Command {
	var category, query string
	var limit, offset int
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list", "search"},
		Short:   "列出 / 搜索素材",
		Example: `  davinci lib ls --category 人像
  davinci lib ls -q "指向 惊讶"      # 多个词要同时命中`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			q := url.Values{}
			if category != "" {
				q.Set("category", category)
			}
			if query == "" && len(args) > 0 {
				query = strings.Join(args, " ")
			}
			if query != "" {
				q.Set("q", query)
			}
			q.Set("limit", fmt.Sprint(limit))
			q.Set("offset", fmt.Sprint(offset))
			var out struct {
				Items []server.LibraryItem `json:"items"`
				Total int                  `json:"total"`
			}
			if err := c.getJSON("/api/library/items?"+q.Encode(), &out); err != nil {
				return err
			}
			if c.JSON {
				printJSON(out)
				return nil
			}
			w := newTabWriter()
			fmt.Fprintln(w, "ID\t分类\t名称\t尺寸\t标签")
			for _, it := range out.Items {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d×%d\t%s\n", it.ID, it.Category, truncate(it.Name, 24), it.Width, it.Height, truncate(strings.Join(it.Tags, ","), 48))
			}
			w.Flush()
			if out.Total > len(out.Items) {
				fmt.Printf("共 %d 条，显示 %d 条（用 --offset 翻页）\n", out.Total, len(out.Items))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&category, "category", "c", "", "分类 id 或名称")
	cmd.Flags().StringVarP(&query, "query", "q", "", "关键词（名称、标签、说明）")
	cmd.Flags().IntVar(&limit, "limit", 100, "最多返回几条")
	cmd.Flags().IntVar(&offset, "offset", 0, "跳过前几条")
	return cmd
}

func newLibShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <素材 id>",
		Short: "看一条素材的完整信息（说明、标签、标注 JSON）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			it, err := c.libItem(args[0])
			if err != nil {
				return err
			}
			if c.JSON {
				printJSON(it)
				return nil
			}
			fmt.Printf("%s  %s\n分类：%s   尺寸：%d×%d\n图片：%s\n", it.ID, it.Name, it.Category, it.Width, it.Height, it.URL)
			if len(it.Tags) > 0 {
				fmt.Printf("标签：%s\n", strings.Join(it.Tags, "、"))
			}
			if it.Description != "" {
				fmt.Printf("\n%s\n", it.Description)
			}
			if len(it.Meta) > 0 {
				fmt.Printf("\n标注：%s\n", compactJSON(it.Meta))
			}
			return nil
		},
	}
}

// uploadLibraryItem posts one file (or remote URL) with its fields.
func (c *Client) uploadLibraryItem(ref string, fields map[string]string) (*server.LibraryItem, bool, error) {
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
			return nil, false, fmt.Errorf("读取 %s: %w", path, err)
		}
		defer f.Close()
		w, err := mw.CreateFormFile("file", filepath.Base(path))
		if err != nil {
			return nil, false, err
		}
		if _, err := io.Copy(w, f); err != nil {
			return nil, false, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, false, err
	}
	resp, err := c.HTTP.Post(c.BaseURL+"/api/library/items", mw.FormDataContentType(), &buf)
	if err != nil {
		return nil, false, fmt.Errorf("上传 %s: %w", ref, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode/100 != 2 {
		return nil, false, apiError(raw, resp.StatusCode)
	}
	var out struct {
		Item    server.LibraryItem `json:"item"`
		Created bool               `json:"created"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false, err
	}
	return &out.Item, out.Created, nil
}

// readArg returns s, or the contents of the file when s is "@path".
func readArg(s string) (string, error) {
	if strings.HasPrefix(s, "@") {
		b, err := os.ReadFile(strings.TrimPrefix(s, "@"))
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return s, nil
}

func newLibAddCmd() *cobra.Command {
	var category, name, tags, desc, meta, source string
	var trim bool
	cmd := &cobra.Command{
		Use:   "add <本地文件|URL>...",
		Short: "往素材库里加素材",
		Example: `  davinci lib add ~/cutouts/*.png --category 人像 --trim
  davinci lib add logo.png --category "APP Logo" --name 飞书 --tags 办公,协作
  davinci lib add p.png --category 人像 --desc @desc.txt --meta @meta.json --source feishu:rec123`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			if category == "" {
				return fmt.Errorf("请用 --category 指定分类（davinci lib cats 看有哪些）")
			}
			d, err := readArg(desc)
			if err != nil {
				return err
			}
			m, err := readArg(meta)
			if err != nil {
				return err
			}
			if len(args) > 1 && (name != "" || source != "") {
				return fmt.Errorf("--name 和 --source 只能在添加单个文件时使用")
			}
			for _, ref := range args {
				it, created, err := c.uploadLibraryItem(ref, map[string]string{
					"category": category, "name": name, "tags": tags, "description": d, "meta": m, "source": source,
					"trim": map[bool]string{true: "true", false: ""}[trim],
				})
				if err != nil {
					return fmt.Errorf("%s: %w", ref, err)
				}
				verb := "已添加"
				if !created {
					verb = "已更新"
				}
				if c.JSON {
					printJSON(it)
				} else {
					fmt.Printf("%s %s  %s（%s，%d×%d）\n", verb, it.ID, it.Name, it.Category, it.Width, it.Height)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&category, "category", "c", "", "分类 id 或名称（必填）")
	cmd.Flags().StringVar(&name, "name", "", "名称（默认用文件名）")
	cmd.Flags().StringVar(&tags, "tags", "", "标签，逗号分隔")
	cmd.Flags().StringVar(&desc, "desc", "", "说明文字，或 @文件")
	cmd.Flags().StringVar(&meta, "meta", "", "附加元数据 JSON，或 @文件")
	cmd.Flags().StringVar(&source, "source", "", "来源标识；同一来源在同一分类只会存一份，重复导入会更新")
	cmd.Flags().BoolVar(&trim, "trim", false, "裁掉 PNG 四周的透明边（抠图常见的大透明画布）")
	return cmd
}

func newLibUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <素材 id>",
		Short: "改素材的名称、分类、标签或说明",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			patch := map[string]any{}
			assign(patch, "name", optStr(cmd, "name"))
			assign(patch, "category", optStr(cmd, "category"))
			if t := optStr(cmd, "tags"); t != nil {
				parts := []string{}
				for _, s := range strings.FieldsFunc(*t, func(r rune) bool { return r == ',' || r == '，' }) {
					parts = append(parts, strings.TrimSpace(s))
				}
				patch["tags"] = parts
			}
			if d := optStr(cmd, "desc"); d != nil {
				v, err := readArg(*d)
				if err != nil {
					return err
				}
				patch["description"] = v
			}
			if len(patch) == 0 {
				return fmt.Errorf("没有要改的内容（--name / --category / --tags / --desc）")
			}
			var out struct {
				Item server.LibraryItem `json:"item"`
			}
			if err := c.sendJSON(http.MethodPatch, "/api/library/items/"+url.PathEscape(args[0]), patch, &out); err != nil {
				return err
			}
			fmt.Printf("已更新 %s  %s（%s）\n", out.Item.ID, out.Item.Name, out.Item.Category)
			return nil
		},
	}
	cmd.Flags().String("name", "", "新名称")
	cmd.Flags().String("category", "", "移到这个分类")
	cmd.Flags().String("tags", "", "替换全部标签，逗号分隔")
	cmd.Flags().String("desc", "", "新的说明文字，或 @文件")
	return cmd
}

func newLibRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <素材 id>...",
		Short: "从素材库删除（已经用在设计里的图片不受影响）",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			for _, id := range args {
				if err := c.delete("/api/library/items/" + url.PathEscape(id)); err != nil {
					return fmt.Errorf("%s: %w", id, err)
				}
				fmt.Printf("已删除 %s\n", id)
			}
			return nil
		},
	}
}

func newLibInsertCmd() *cobra.Command {
	var x, y, w, h float64
	var name string
	cmd := &cobra.Command{
		Use:   "insert <素材 id>",
		Short: "把素材插入当前项目",
		Long: `把素材作为图片图层插入当前项目（-p 指定项目）。

不给 --w/--h 时等比缩放到能放进画布的 80%；不给 --x/--y 时水平居中，人像贴底
（封面的常见摆法），其他素材垂直居中。和 layer add-image 一样是一条 addImage 命令，可以撤销。`,
		Example: `  davinci lib insert lib_xxx
  davinci lib insert lib_xxx --h 1100 --x 500      # 指定高度，放右半边`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			it, err := c.libItem(args[0])
			if err != nil {
				return err
			}
			id, err := c.ProjectID()
			if err != nil {
				return err
			}
			var proj struct {
				Project struct {
					Width  int `json:"width"`
					Height int `json:"height"`
				} `json:"project"`
			}
			if err := c.getJSON("/api/projects/"+id, &proj); err != nil {
				return err
			}
			cw, ch := float64(proj.Project.Width), float64(proj.Project.Height)
			iw, ih := float64(it.Width), float64(it.Height)
			fw, fh := optFloat(cmd, "w"), optFloat(cmd, "h")
			var width, height float64
			switch {
			case fw != nil && fh != nil:
				width, height = *fw, *fh
			case fw != nil && iw > 0:
				width, height = *fw, *fw*ih/iw
			case fh != nil && ih > 0:
				width, height = *fh*iw/ih, *fh
			case iw > 0 && ih > 0:
				k := math.Min(1, math.Min(cw*0.8/iw, ch*0.8/ih))
				width, height = iw*k, ih*k
			}
			m := map[string]any{"type": "addImage", "url": it.URL, "name": firstNonEmpty(name, it.Name)}
			if width > 0 && height > 0 {
				m["width"] = math.Round(width)
				m["height"] = math.Round(height)
				m["x"] = math.Round((cw - width) / 2)
				// A portrait stands on the bottom edge, the way a cover uses one;
				// anything else goes in the middle.
				if it.Category == "人像" {
					m["y"] = math.Round(ch - height)
				} else {
					m["y"] = math.Round((ch - height) / 2)
				}
			}
			assign(m, "x", optFloat(cmd, "x"))
			assign(m, "y", optFloat(cmd, "y"))
			return c.execAndPrint(m)
		},
	}
	cmd.Flags().Float64Var(&x, "x", 0, "左边距（默认水平居中）")
	cmd.Flags().Float64Var(&y, "y", 0, "上边距（默认人像贴底、其他居中）")
	cmd.Flags().Float64Var(&w, "w", 0, "宽度（默认等比缩进画布的 80%）")
	cmd.Flags().Float64Var(&h, "h", 0, "高度")
	cmd.Flags().StringVar(&name, "name", "", "图层名称（默认用素材名）")
	return cmd
}
