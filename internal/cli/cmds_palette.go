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
	"sort"
	"strings"

	"davinci/internal/server"
	"github.com/spf13/cobra"
)

// paletteID accepts an ID or an exact name. IDs take precedence over names.
func (c *Client) paletteID(ref string) (string, error) {
	var out struct {
		Palettes []server.Palette `json:"palettes"`
	}
	if err := c.getJSON("/api/palettes", &out); err != nil {
		return "", err
	}
	var matches []string
	for _, p := range out.Palettes {
		if p.ID == ref {
			return p.ID, nil
		}
		if p.Name == ref {
			matches = append(matches, p.ID)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return "", fmt.Errorf("配色名称 %q 重名，请使用候选 id：%s", ref, strings.Join(matches, ", "))
	}
	return "", fmt.Errorf("找不到配色：%s（用 `davinci palette list` 查看）", ref)
}

func (c *Client) printPalette(p any) {
	if c.JSON {
		printJSON(p)
		return
	}
	switch v := p.(type) {
	case server.Palette:
		fmt.Printf("%s  %s\nbg %s  text %s  highlight %s  text2 %s  accent %s\n", v.ID, v.Name, v.BG, v.Text, v.Highlight, v.Text2, v.Accent)
		if v.ImageURL != "" {
			fmt.Printf("原图 %s%s\n", c.BaseURL, v.ImageURL)
		}
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
	cmd.AddCommand(&cobra.Command{Use: "show <id或名称>", Short: "配色详情", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := NewClient()
		if err != nil {
			return err
		}
		var out struct {
			Palette server.Palette `json:"palette"`
		}
		id, err := c.paletteID(args[0])
		if err != nil {
			return err
		}
		if err := c.getJSON("/api/palettes/"+url.PathEscape(id), &out); err != nil {
			return err
		}
		c.printPalette(out.Palette)
		return nil
	}})
	cmd.AddCommand(newPaletteAddCmd(), newPaletteUpdateCmd())
	cmd.AddCommand(&cobra.Command{Use: "delete <id或名称>", Short: "删除配色（已应用的画板不受影响）", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := NewClient()
		if err != nil {
			return err
		}
		id, err := c.paletteID(args[0])
		if err != nil {
			return err
		}
		if err := c.delete("/api/palettes/" + url.PathEscape(id)); err != nil {
			return err
		}
		if c.JSON {
			printJSON(map[string]any{"ok": true, "id": id})
		} else {
			fmt.Printf("已删除配色 %s\n", id)
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "apply <id或名称>", Short: "应用到当前画板（-p 指定项目，--board 指定画板，一步撤销）", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := NewClient()
		if err != nil {
			return err
		}
		id, err := c.paletteID(args[0])
		if err != nil {
			return err
		}
		return c.execAndPrint(map[string]any{"type": "applyPalette", "id": id})
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
		if err := c.sendPaletteImage(http.MethodPost, "/api/palettes", path, draft, &out); err != nil {
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
	cmd := &cobra.Command{Use: "update <id或名称>", Short: "重命名、修改角色色或替换原图", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		patch := map[string]string{}
		for _, key := range []string{"name", "bg", "text", "highlight", "text2", "accent"} {
			if v := optStr(cmd, key); v != nil {
				patch[key] = *v
			}
		}
		path, _ := cmd.Flags().GetString("image")
		reextract, _ := cmd.Flags().GetBool("reextract")
		if reextract && path == "" {
			return fmt.Errorf("--reextract 需要 --image")
		}
		if len(patch) == 0 && path == "" {
			return fmt.Errorf("请指定 --name、角色色或 --image")
		}
		c, err := NewClient()
		if err != nil {
			return err
		}
		var out struct {
			Palette server.Palette `json:"palette"`
		}
		id, err := c.paletteID(args[0])
		if err != nil {
			return err
		}
		if reextract {
			draft, err := c.analyzePalette(path, "")
			if err != nil {
				return err
			}
			for key, value := range map[string]string{"bg": draft.BG, "text": draft.Text, "highlight": draft.Highlight, "text2": draft.Text2, "accent": draft.Accent} {
				if _, exists := patch[key]; !exists {
					patch[key] = value
				}
			}
		}
		endpoint := "/api/palettes/" + url.PathEscape(id)
		if path != "" {
			err = c.sendPaletteImage(http.MethodPatch, endpoint, path, patch, &out)
		} else {
			err = c.sendJSON(http.MethodPatch, endpoint, patch, &out)
		}
		if err != nil {
			return err
		}
		c.printPalette(out.Palette)
		return nil
	}}
	cmd.Flags().String("name", "", "配色名称")
	cmd.Flags().String("image", "", "设置或替换原图（默认保留颜色）")
	cmd.Flags().Bool("reextract", false, "使用 --image 重新提色，显式角色色优先")
	paletteColorFlags(cmd)
	return cmd
}
func (c *Client) analyzePalette(path, name string) (*server.PaletteDraft, error) {
	var out struct {
		Palette server.PaletteDraft `json:"palette"`
	}
	if err := c.paletteMultipart(http.MethodPost, "/api/palettes/analyze", path, map[string]string{"name": name}, &out); err != nil {
		return nil, err
	}
	return &out.Palette, nil
}
func (c *Client) sendPaletteImage(method, endpoint, path string, palette, out any) error {
	raw, err := json.Marshal(palette)
	if err != nil {
		return err
	}
	return c.paletteMultipart(method, endpoint, path, map[string]string{"palette": string(raw)}, out)
}
func (c *Client) paletteMultipart(method, endpoint, path string, fields map[string]string, out any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > 20<<20 {
		return fmt.Errorf("图片不能超过 20 MB")
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for key, value := range fields {
		if err := mw.WriteField(key, value); err != nil {
			return err
		}
	}
	fw, err := mw.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return err
	}
	n, err := io.Copy(fw, io.LimitReader(f, (20<<20)+1))
	if err != nil {
		return err
	}
	if n > 20<<20 {
		return fmt.Errorf("图片不能超过 20 MB")
	}
	if err := mw.Close(); err != nil {
		return err
	}
	req, err := http.NewRequest(method, c.BaseURL+endpoint, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return apiError(raw, resp.StatusCode)
	}
	return json.Unmarshal(raw, out)
}
