package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Background removal runs rembg (https://github.com/danielgatis/rembg) with a
// BiRefNet model as a command-line tool. A cut-out is kept under
// data/rembg/<source sha>-<model>.png, so cutting the same picture again —
// an undo and redo, the same photo in another project — is instant.
//
// One cut at a time: a BiRefNet run holds a couple of GB of memory.

var rembgMu sync.Mutex

const rembgTimeout = 10 * time.Minute

// rembgModels are the models the removeBackground command may ask for.
var rembgModels = map[string]bool{"birefnet-general": true, "birefnet-portrait": true}

// rembgBinary finds the rembg command: DAVINCI_REMBG, else rembg on PATH.
func rembgBinary() (string, error) {
	if p := strings.TrimSpace(os.Getenv("DAVINCI_REMBG")); p != "" {
		return p, nil
	}
	if p, err := exec.LookPath("rembg"); err == nil {
		return p, nil
	}
	return "", errors.New(`没找到 rembg：先安装（pip install "rembg[cpu,cli]"），或用环境变量 DAVINCI_REMBG 指定它的路径`)
}

// removeBackground cuts the subject out of a picture and stores the result
// as a new asset.
func (s *Server) removeBackground(ref, model string) (string, int, int, error) {
	if !rembgModels[model] {
		return "", 0, 0, fmt.Errorf("unknown background removal model %q", model)
	}
	url, as, err := s.assets.Add(ref)
	if err != nil {
		return "", 0, 0, err
	}
	src, err := s.assets.Path(url)
	if err != nil {
		return "", 0, 0, err
	}
	dir := filepath.Join(s.dataDir, "rembg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, 0, err
	}
	out := filepath.Join(dir, as.SHA+"-"+model+".png")
	if _, err := os.Stat(out); err != nil {
		if err := runRembg(src, out, model); err != nil {
			return "", 0, 0, err
		}
	}
	b, err := os.ReadFile(out)
	if err != nil {
		return "", 0, 0, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return "", 0, 0, fmt.Errorf("rembg wrote an unreadable image: %w", err)
	}
	cut, err := s.assets.SaveBytes(b, "png")
	if err != nil {
		return "", 0, 0, err
	}
	return "/assets/" + cut.SHA + ".png", cfg.Width, cfg.Height, nil
}

func runRembg(src, out, model string) error {
	bin, err := rembgBinary()
	if err != nil {
		return err
	}
	rembgMu.Lock()
	defer rembgMu.Unlock()
	if _, err := os.Stat(out); err == nil {
		return nil // cut while we waited
	}
	ctx, cancel := context.WithTimeout(context.Background(), rembgTimeout)
	defer cancel()
	tmp := out + ".tmp.png"
	defer os.Remove(tmp)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "i", "-m", model, src, tmp)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("去除背景超时（%s）", rembgTimeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return fmt.Errorf("rembg 失败：%v %s", err, msg)
	}
	return os.Rename(tmp, out)
}
