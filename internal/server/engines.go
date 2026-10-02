package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"sync"
	"time"

	"davinci/internal/doc"
	"davinci/internal/render"

	_ "golang.org/x/image/webp"
)

// The server owns every document. An edit — from the editor page, the CLI or
// an AI agent — is a command applied by the project's engine; the result is
// saved and pushed to every page showing the project, which only draws it.

// Result is what a command produced, plus the document it left behind so a
// caller never has to re-query.
type Result struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data,omitempty"`
	Error    string          `json:"error,omitempty"`
	Document json.RawMessage `json:"document,omitempty"`
	Revision int64           `json:"revision,omitempty"`
	History  *History        `json:"history,omitempty"`
}

// History says whether undo and redo have anything to take back.
type History struct {
	CanUndo bool `json:"canUndo"`
	CanRedo bool `json:"canRedo"`
}

func historyOf(e *doc.Engine) *History {
	return &History{CanUndo: e.CanUndo(), CanRedo: e.CanRedo()}
}

// engines holds one engine per open project.
type engines struct {
	mu     sync.Mutex
	byID   map[string]*doc.Engine
	thumbs map[string]*time.Timer
}

// engine returns the project's engine, loading the document on first use.
func (s *Server) engine(projectID string) (*doc.Engine, error) {
	s.eng.mu.Lock()
	defer s.eng.mu.Unlock()
	if e := s.eng.byID[projectID]; e != nil {
		return e, nil
	}
	p, err := s.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("project not found: %s", projectID)
	}
	var d *doc.Project
	if len(p.Document) > 0 {
		d, err = doc.Parse(p.Document)
	}
	if d == nil || err != nil {
		w, h := float64(p.Width), float64(p.Height)
		if w <= 0 || h <= 0 {
			w, h = 1242, 1656
		}
		d = doc.Blank(w, h)
	}
	e := doc.NewEngine(d, s.services())
	s.eng.byID[projectID] = e
	return e, nil
}

// forget drops a project's engine (deleted, or replaced by an import).
func (s *Server) forget(projectID string) {
	s.eng.mu.Lock()
	delete(s.eng.byID, projectID)
	if t := s.eng.thumbs[projectID]; t != nil {
		t.Stop()
		delete(s.eng.thumbs, projectID)
	}
	s.eng.mu.Unlock()
}

// Exec applies one command (or a batch) to a project, saves and broadcasts.
func (s *Server) Exec(projectID string, cmd json.RawMessage) (*Result, error) {
	return s.ExecFrom(projectID, cmd, "")
}

// ExecFrom is Exec for a command from an editor page: the change goes out
// marked with the page's origin, so that page knows the edit was its own.
func (s *Server) ExecFrom(projectID string, cmd json.RawMessage, origin string) (*Result, error) {
	if len(cmd) == 0 {
		return nil, errors.New("empty command")
	}
	e, err := s.engine(projectID)
	if err != nil {
		return nil, err
	}
	r := e.Apply(cmd)
	res := &Result{OK: r.OK, Error: r.Error}
	if r.Data != nil {
		res.Data, _ = json.Marshal(r.Data)
	}
	snapshot := e.Snapshot()
	res.Document = snapshot
	res.History = historyOf(e)
	if r.Changed {
		rev, err := s.persist(projectID, snapshot, e, origin, cmd)
		if err != nil {
			return nil, err
		}
		res.Revision = rev
	} else if p, _ := s.store.GetProject(projectID); p != nil {
		res.Revision = p.Revision
	}
	return res, nil
}

// ExecBatch runs several commands as one undo step.
func (s *Server) ExecBatch(projectID string, cmds []json.RawMessage) (*Result, error) {
	if len(cmds) == 0 {
		return nil, errors.New("empty batch")
	}
	batch, err := json.Marshal(map[string]any{"type": "batch", "commands": cmds})
	if err != nil {
		return nil, err
	}
	return s.Exec(projectID, batch)
}

// ReplaceDocument swaps a project's whole document (an import), undoably.
func (s *Server) ReplaceDocument(projectID string, raw json.RawMessage) error {
	d, err := doc.Parse(raw)
	if err != nil {
		return fmt.Errorf("not a davinci document: %w", err)
	}
	e, err := s.engine(projectID)
	if err != nil {
		return err
	}
	e.Replace(d)
	_, err = s.persist(projectID, e.Snapshot(), e, "", nil)
	return err
}

// persist saves a document, tells every open page (with the command that made
// the change and where it came from), and schedules a new tile.
func (s *Server) persist(projectID string, snapshot []byte, e *doc.Engine, origin string, cmd json.RawMessage) (int64, error) {
	w, h := canvasSizeOf(snapshot)
	rev, err := s.store.SaveDocument(projectID, snapshot, w, h)
	if err != nil {
		return 0, err
	}
	s.hub.broadcast(projectID, Envelope{Type: "doc", Document: snapshot, Revision: rev, Origin: origin, Command: cmd, History: historyOf(e)})
	s.scheduleThumbnail(projectID)
	return rev, nil
}

// scheduleThumbnail redraws the project's home-page tile once edits settle.
func (s *Server) scheduleThumbnail(projectID string) {
	s.eng.mu.Lock()
	defer s.eng.mu.Unlock()
	if t := s.eng.thumbs[projectID]; t != nil {
		t.Stop()
	}
	s.eng.thumbs[projectID] = time.AfterFunc(1500*time.Millisecond, func() {
		if err := s.updateThumbnail(projectID); err != nil {
			s.logf("[thumb] project %s: %v", projectID, err)
		}
	})
}

// updateThumbnail draws the first board (the project's face) 240 px wide.
func (s *Server) updateThumbnail(projectID string) error {
	e, err := s.engine(projectID)
	if err != nil {
		return err
	}
	p := e.Project()
	b := p.Boards[0]
	scale := 240 / b.Canvas.Width
	if scale > 1 {
		scale = 1
	}
	img, err := s.renderer.Render(b, doc.RenderOptions{Scale: scale, Format: "jpeg", Quality: 0.82})
	if err != nil {
		return err
	}
	return s.store.SetThumbnail(projectID, "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(img))
}

// --- what commands need from the server ------------------------------------------

type services struct{ s *Server }

func (s *Server) services() doc.Services { return services{s} }

func (v services) Measure(layers []*doc.Layer) (map[string]doc.TextSize, error) {
	return v.s.renderer.Measure(layers)
}

func (v services) Render(b *doc.Board, opts doc.RenderOptions) ([]byte, error) {
	return v.s.renderer.Render(b, opts)
}

// Fonts lists the families a design should pick from: favourites first,
// hidden ones left out.
func (v services) Fonts() []string {
	var out []string
	for _, f := range v.s.fontList() {
		if !f.Hidden {
			out = append(out, f.Family)
		}
	}
	return out
}

// Asset stores an image reference and reports its pixel size.
func (v services) RemoveBackground(ref, model string) (string, int, int, error) {
	return v.s.removeBackground(ref, model)
}

func (v services) Asset(ref string) (string, int, int, error) {
	url, _, err := v.s.assets.Add(ref)
	if err != nil {
		return "", 0, 0, err
	}
	path, err := v.s.assets.Path(url)
	if err != nil {
		return "", 0, 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return "", 0, 0, fmt.Errorf("could not read image %s: %w", ref, err)
	}
	return url, cfg.Width, cfg.Height, nil
}

var _ = render.ErrUnavailable
