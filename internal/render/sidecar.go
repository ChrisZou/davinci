// Package render runs the server-side renderer: the editor's own CanvasKit
// drawing code (web/src/render), in a Node process the server starts on first
// use. It measures text for the document and draws exports and thumbnails.
package render

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"davinci/internal/doc"
	"davinci/web"
)

// Sidecar is one Node renderer process, started on demand, stopped when idle.
type Sidecar struct {
	dataDir string
	baseURL string
	logf    func(string, ...any)
	idle    time.Duration

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	out     *bufio.Reader
	nextID  int
	lastUse time.Time
	done    chan struct{}
}

// ErrUnavailable means no renderer can run here (no Node, no built renderer).
var ErrUnavailable = errors.New("renderer unavailable")

// New prepares a renderer that fetches fonts and images from baseURL.
func New(dataDir, baseURL string, logf func(string, ...any)) *Sidecar {
	s := &Sidecar{dataDir: dataDir, baseURL: baseURL, logf: logf, idle: 10 * time.Minute, done: make(chan struct{})}
	go s.reap()
	return s
}

// Measure lays out text layers and reports their box sizes.
func (s *Sidecar) Measure(layers []*doc.Layer) (map[string]doc.TextSize, error) {
	raw, err := s.call(map[string]any{"op": "measure", "layers": layers}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	var out map[string]doc.TextSize
	return out, json.Unmarshal(raw, &out)
}

// Render draws a board as an encoded image.
func (s *Sidecar) Render(b *doc.Board, opts doc.RenderOptions) ([]byte, error) {
	// CanvasKit's stock build encodes PNG (and WebP) but not JPEG: ask for a
	// PNG and convert here when a JPEG is wanted.
	format := opts.Format
	if format == "jpeg" || format == "jpg" {
		format = "png"
	}
	req := map[string]any{
		"op":          "render",
		"board":       map[string]any{"canvas": b.Canvas, "layers": b.Layers},
		"scale":       opts.Scale,
		"format":      format,
		"quality":     opts.Quality,
		"transparent": opts.Transparent,
	}
	if len(opts.IDs) > 0 {
		req["ids"] = opts.IDs
	}
	raw, err := s.call(req, 120*time.Second)
	if err != nil {
		return nil, err
	}
	var res struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	out, err := base64.StdEncoding.DecodeString(res.Data)
	if err != nil || (opts.Format != "jpeg" && opts.Format != "jpg") {
		return out, err
	}
	return toJPEG(out, opts.Quality, opts.Transparent)
}

// toJPEG re-encodes a PNG as a JPEG, flattening transparency onto white.
func toJPEG(pngBytes []byte, quality float64, _ bool) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	flat := image.NewRGBA(b)
	draw.Draw(flat, b, image.White, image.Point{}, draw.Src)
	draw.Draw(flat, b, src, b.Min, draw.Over)
	q := int(quality * 100)
	if q <= 0 || q > 100 {
		q = 92
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: q}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Close stops the renderer.
func (s *Sidecar) Close() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

func (s *Sidecar) call(req map[string]any, timeout time.Duration) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.startLocked(); err != nil {
		return nil, err
	}
	s.lastUse = time.Now()
	s.nextID++
	req["id"] = s.nextID
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := s.stdin.Write(append(line, '\n')); err != nil {
		s.stopLocked()
		return nil, fmt.Errorf("renderer: %w", err)
	}
	type reply struct {
		ID     int             `json:"id"`
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	got := make(chan reply, 1)
	fail := make(chan error, 1)
	out := s.out
	want := s.nextID
	go func() {
		for {
			b, err := out.ReadBytes('\n')
			if err != nil {
				fail <- err
				return
			}
			var r reply
			if json.Unmarshal(b, &r) != nil || r.ID != want {
				continue
			}
			got <- r
			return
		}
	}()
	select {
	case r := <-got:
		if !r.OK {
			return nil, errors.New(r.Error)
		}
		return r.Result, nil
	case err := <-fail:
		s.stopLocked()
		return nil, fmt.Errorf("renderer exited: %w", err)
	case <-time.After(timeout):
		// A wedged renderer is replaced rather than waited on.
		s.stopLocked()
		return nil, errors.New("renderer timed out")
	}
}

func (s *Sidecar) startLocked() error {
	if s.cmd != nil {
		return nil
	}
	node := os.Getenv("DAVINCI_NODE")
	if node == "" {
		p, err := exec.LookPath("node")
		if err != nil {
			return fmt.Errorf("%w: Node.js is not installed", ErrUnavailable)
		}
		node = p
	}
	script, wasm, err := web.RendererFiles()
	if err != nil {
		return fmt.Errorf("%w: this build has no renderer (run ./start.sh build)", ErrUnavailable)
	}
	sum := sha1.Sum(append(append([]byte{}, script...), wasm[:1024]...))
	dir := filepath.Join(s.dataDir, ".renderer", hex.EncodeToString(sum[:6]))
	if err := writeOnce(filepath.Join(dir, "renderer.cjs"), script); err != nil {
		return err
	}
	if err := writeOnce(filepath.Join(dir, "canvaskit.wasm"), wasm); err != nil {
		return err
	}
	cmd := exec.Command(node, filepath.Join(dir, "renderer.cjs"))
	cmd.Env = append(os.Environ(), "DAVINCI_URL="+s.baseURL, "CANVASKIT_WASM="+filepath.Join(dir, "canvaskit.wasm"))
	cmd.Stderr = logWriter{s.logf}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start renderer: %w", err)
	}
	out := bufio.NewReaderSize(stdout, 1<<20)
	// The first line says it is ready (CanvasKit has loaded).
	ready := make(chan error, 1)
	go func() {
		_, err := out.ReadBytes('\n')
		ready <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			_ = cmd.Process.Kill()
			return fmt.Errorf("renderer did not start: %w", err)
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		return errors.New("renderer did not start in time")
	}
	s.cmd, s.stdin, s.out = cmd, stdin, out
	s.logf("[render] renderer started (pid %d)", cmd.Process.Pid)
	return nil
}

func (s *Sidecar) stopLocked() {
	if s.cmd == nil {
		return
	}
	_ = s.stdin.Close()
	done := make(chan struct{})
	go func() { _ = s.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	}
	s.cmd, s.stdin, s.out = nil, nil, nil
}

// reap stops an idle renderer: it holds ~100 MB and fonts, and the next use
// starts it again in well under a second.
func (s *Sidecar) reap() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.mu.Lock()
			if s.cmd != nil && time.Since(s.lastUse) > s.idle {
				s.logf("[render] renderer idle, stopping")
				s.stopLocked()
			}
			s.mu.Unlock()
		}
	}
}

func writeOnce(path string, b []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type logWriter struct{ logf func(string, ...any) }

func (w logWriter) Write(p []byte) (int, error) {
	for _, line := range bytes.Split(bytes.TrimRight(p, "\n"), []byte("\n")) {
		if len(line) > 0 {
			w.logf("[render] %s", line)
		}
	}
	return len(p), nil
}
