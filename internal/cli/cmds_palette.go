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

	"davinci/internal/server"
	"github.com/spf13/cobra"
)

func (c *Client) printPalette(p any) {
	if c.JSON {
		printJSON(p)
		return
	}
	switch v := p.(type) {
	case server.Palette:
		fmt.Printf("%s  %s\nbg %s  text %s  highlight %s  text2 %s  accent %s\n", v.ID, v.Name, v.BG, v.Text, v.Highlight, v.Text2, v.Accent)
	case server.PaletteDraft:
		fmt.Printf("%s\nbg %s  text %s  highlight %s  text2 %s  accent %s\n", v.Name, v.BG, v.Text, v.Highlight, v.Text2, v.Accent)
	}
}
func newPaletteCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "palette", Short: "全局配色：从图片解析、管理与应用"}
	list := &cobra.Command{Use: "list", Short: "列出配色", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		c, err := NewClient()
		if err != nil {
			return err
		}
		var out struct {
			Palettes []server.Palette `json:"palettes"`
		}
		if err := c.getJSON("/api/palettes", &out); err != nil {
			return err
		}
		query, _ := cmd.Flags().GetString("query")
		if query = strings.ToLower(strings.TrimSpace(query)); query != "" {
			filtered := []server.Palette{}
			for _, p := range out.Palettes {
				if strings.Contains(strings.ToLower(p.Name), query) {
					filtered = append(filtered, p)
				}
			}
			out.Palettes = filtered
		}

		if c.JSON {
			printJSON(out)
			return nil
		}
		w := newTabWriter()
		defer w.Flush()
		fmt.Fprintln(w, "ID\t名称\t背景\t主文字\t高亮\t次文字\t点缀")
		for _, p := range out.Palettes {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.ID, p.Name, p.BG, p.Text, p.Highlight, p.Text2, p.Accent)
		}
		return nil
	}}
	list.Flags().StringP("query", "q", "", "按名称搜索配色")
	cmd.AddCommand(list)
	cmd.AddCommand(&cobra.Command{Use: "show <id>", Short: "配色详情", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := NewClient()
		if err != nil {
			return err
		}
		var out struct {
			Palette server.Palette `json:"palette"`
		}
		if err := c.getJSON("/api/palettes/"+url.PathEscape(args[0]), &out); err != nil {
			return err
		}
		c.printPalette(out.Palette)
		return nil
	}})
	cmd.AddCommand(newPaletteAddCmd(), newPaletteUpdateCmd())
	cmd.AddCommand(&cobra.Command{Use: "delete <id>", Short: "删除配色（已应用的画板不受影响）", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := NewClient()
		if err != nil {
			return err
		}
		if err := c.delete("/api/palettes/" + url.PathEscape(args[0])); err != nil {
			return err
		}
		if c.JSON {
			printJSON(map[string]any{"ok": true, "id": args[0]})
		} else {
			fmt.Printf("已删除配色 %s\n", args[0])
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "apply <id>", Short: "应用到当前画板（-p 指定项目，--board 指定画板，一步撤销）", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := NewClient()
		if err != nil {
			return err
		}
		return c.execAndPrint(map[string]any{"type": "applyPalette", "id": args[0]})
	}})
	return cmd
}
func paletteColorFlags(cmd *cobra.Command) {
	for _, key := range []string{"bg", "text", "highlight", "text2", "accent"} {
		cmd.Flags().String(key, "", "角色色 #RRGGBB")
	}
}
func newPaletteAddCmd() *cobra.Command {
	var path, name string
	var dry bool
	cmd := &cobra.Command{Use: "add --from-image <path>", Short: "从图片解析配色并保存（--dry-run 只预览）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if path == "" {
			return fmt.Errorf("请用 --from-image 指定图片")
		}
		c, err := NewClient()
		if err != nil {
			return err
		}
		draft, err := c.analyzePalette(path, name)
		if err != nil {
			return err
		}
		for key, ptr := range map[string]*string{"bg": &draft.BG, "text": &draft.Text, "highlight": &draft.Highlight, "text2": &draft.Text2, "accent": &draft.Accent} {
			if v := optStr(cmd, key); v != nil {
				*ptr = *v
			}
		}
		if err := draft.PaletteColors.Normalize(); err != nil {
			return err
		}
		if dry {
			c.printPalette(*draft)
			return nil
		}
		var out struct {
			Palette server.Palette `json:"palette"`
		}
		if err := c.postJSON("/api/palettes", draft, &out); err != nil {
			return err
		}
		c.printPalette(out.Palette)
		return nil
	}}
	cmd.Flags().StringVar(&path, "from-image", "", "本地图片路径（PNG/JPEG/GIF/WebP）")
	cmd.Flags().StringVar(&name, "name", "", "配色名称（默认文件名）")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "只打印解析结果，不保存")
	paletteColorFlags(cmd)
	return cmd
}
func newPaletteUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "update <id>", Short: "重命名或修改角色色", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		patch := map[string]string{}
		for _, key := range []string{"name", "bg", "text", "highlight", "text2", "accent"} {
			if v := optStr(cmd, key); v != nil {
				patch[key] = *v
			}
		}
		if len(patch) == 0 {
			return fmt.Errorf("请指定 --name 或角色色")
		}
		c, err := NewClient()
		if err != nil {
			return err
		}
		var out struct {
			Palette server.Palette `json:"palette"`
		}
		if err := c.sendJSON(http.MethodPatch, "/api/palettes/"+url.PathEscape(args[0]), patch, &out); err != nil {
			return err
		}
		c.printPalette(out.Palette)
		return nil
	}}
	cmd.Flags().String("name", "", "配色名称")
	paletteColorFlags(cmd)
	return cmd
}
func (c *Client) analyzePalette(path, name string) (*server.PaletteDraft, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > 20<<20 {
		return nil, fmt.Errorf("图片不能超过 20 MB")
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("name", name); err != nil {
		return nil, err
	}
	fw, err := mw.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(fw, io.LimitReader(f, (20<<20)+1))
	if err != nil {
		return nil, err
	}
	if n > 20<<20 {
		return nil, fmt.Errorf("图片不能超过 20 MB")
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Post(c.BaseURL+"/api/palettes/analyze", mw.FormDataContentType(), &buf)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, apiError(raw, resp.StatusCode)
	}
	var out struct {
		Palette server.PaletteDraft `json:"palette"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out.Palette, nil
}
