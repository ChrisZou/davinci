package cli

import (
	"archive/zip"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newRenderCmd() *cobra.Command {
	var out, format string
	var scale, quality float64
	var openIt, allBoards, transparent, clipboard bool
	var ids []string
	cmd := &cobra.Command{
		Use:     "render [-o out.png|目录|out.zip] [--all-boards]",
		Aliases: []string{"export", "render-png"},
		Short:   "导出当前/指定画板图片，或所有画板到目录/ZIP（服务端渲染）",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			format = strings.ToLower(format)
			if format != "png" && format != "jpg" && format != "jpeg" && format != "webp" {
				return fmt.Errorf("format 必须为 png|jpg|jpeg|webp")
			}
			if math.IsNaN(scale) || math.IsInf(scale, 0) || scale <= 0 || scale > 8 {
				return fmt.Errorf("scale 必须大于 0 且不超过 8")
			}
			if math.IsNaN(quality) || math.IsInf(quality, 0) || quality < 0.05 || quality > 1 {
				return fmt.Errorf("quality 必须在 0.05–1 之间")
			}
			if allBoards && (gf.board != "" || cmd.Flags().Changed("ids") || clipboard) {
				return fmt.Errorf("--all-boards 不能与 --board、--ids 或 --clipboard 同用")
			}
			if !allBoards && strings.EqualFold(filepath.Ext(out), ".zip") {
				return fmt.Errorf("输出 ZIP 请加 --all-boards")
			}
			if cmd.Flags().Changed("ids") && len(ids) == 0 {
				return fmt.Errorf("--ids 至少指定一个图层 id 或名称")
			}
			c, err := NewClient()
			if err != nil {
				return err
			}
			id, err := c.ProjectID()
			if err != nil {
				return err
			}
			q := url.Values{"scale": {strconv.FormatFloat(scale, 'f', -1, 64)}, "quality": {strconv.FormatFloat(quality, 'f', -1, 64)}, "transparent": {strconv.FormatBool(transparent)}}
			if gf.board != "" {
				q.Set("board", gf.board)
			}
			for _, layer := range ids {
				q.Add("ids", layer)
			}
			ext := format
			if allBoards {
				ext = "zip"
				q.Set("format", format)
			}
			if clipboard {
				ext = "png"
			}
			resp, err := c.HTTP.Get(c.BaseURL + "/api/projects/" + id + "/export." + ext + "?" + q.Encode())
			if err != nil {
				return fmt.Errorf("渲染请求失败: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode/100 != 2 {
				return readAPIError(resp)
			}
			// Download completely before touching the destination; failed renders must
			// never truncate a previous export. Keep large archives off the Go heap.
			tmp, err := os.CreateTemp("", "davinci-export-*."+ext)
			if err != nil {
				return err
			}
			defer os.Remove(tmp.Name())
			n, err := io.Copy(tmp, resp.Body)
			closeErr := tmp.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if clipboard {
				if err := copyImage(tmp.Name()); err != nil {
					return err
				}
				fmt.Println("已复制 PNG 到剪贴板")
				if out == "" {
					return nil
				}
			}
			if out == "" {
				out = "davinci-" + id + "." + format
				if allBoards {
					out = "davinci-" + id
				}
			}
			if allBoards && !strings.EqualFold(filepath.Ext(out), ".zip") {
				count, err := extractBoardArchive(tmp.Name(), out)
				if err != nil {
					return err
				}
				fmt.Printf("已导出 %s（%d 个画板）\n", out, count)
			} else {
				if err := copyExportFile(tmp.Name(), out); err != nil {
					return err
				}
				fmt.Printf("已导出 %s（%s）\n", out, humanBytes(n))
			}
			if openIt {
				openBrowser(out)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", "输出文件；--all-boards 时为目录或 .zip 文件")
	cmd.Flags().Float64Var(&scale, "scale", 1, "导出倍率（如 1、2、3，最大 8）")
	cmd.Flags().StringVar(&format, "format", "png", "png|jpg|jpeg|webp（不从输出扩展名推断）")
	cmd.Flags().Float64Var(&quality, "quality", 0.92, "JPG/WebP 质量 0.05–1（面板范围 0.3–1）")
	cmd.Flags().BoolVar(&transparent, "transparent", false, "不画背景色/背景图；PNG/WebP 支持透明")
	cmd.Flags().StringSliceVar(&ids, "ids", nil, "只导出这些图层并裁到外框：id/名称，逗号分隔，可重复")
	cmd.Flags().BoolVar(&allBoards, "all-boards", false, "按画板顺序导出所有画板，与 --board、--ids、--clipboard 互斥")
	cmd.Flags().BoolVar(&clipboard, "clipboard", false, "复制当前/指定画板为 PNG；指定 -o 时同时保存 PNG")
	cmd.Flags().BoolVar(&openIt, "open", false, "导出后打开")
	return cmd
}

func copyExportFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	// Create beside the destination, then rename to avoid partial files and avoid
	// following an existing destination symlink.
	out, err := os.CreateTemp(filepath.Dir(dst), ".davinci-export-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Chmod(0o644)
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(out.Name(), dst)
}

func extractBoardArchive(src, dst string) (int, error) {
	archive, err := zip.OpenReader(src)
	if err != nil {
		return 0, err
	}
	defer archive.Close()
	// Validate before writing. Archives contain only flat image entries; a remote
	// server must not be able to write outside the requested directory.
	for _, f := range archive.File {
		if f.Name == "." || f.Name == ".." || strings.ContainsAny(f.Name, `/\:`) || !filepath.IsLocal(f.Name) || !f.Mode().IsRegular() {
			return 0, fmt.Errorf("无效的 ZIP 文件名: %q", f.Name)
		}
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(dst)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	for _, f := range archive.File {
		in, err := f.Open()
		if err != nil {
			return 0, err
		}
		// Never follow a pre-existing symlink, including one pointing inside dst.
		if st, e := root.Lstat(f.Name); e == nil && st.Mode()&os.ModeSymlink != 0 {
			in.Close()
			return 0, fmt.Errorf("拒绝覆盖符号链接: %s", f.Name)
		}
		out, err := root.OpenFile(f.Name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			in.Close()
			return 0, err
		}
		_, err = io.Copy(out, in)
		in.Close()
		closeErr := out.Close()
		if err != nil {
			return 0, err
		}
		if closeErr != nil {
			return 0, closeErr
		}
	}
	return len(archive.File), nil
}
