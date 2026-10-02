package doc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Services is everything a command needs from outside the document.
type Services interface {
	// Measure lays out text layers and reports each one's box height.
	Measure(layers []*Layer) (map[string]TextSize, error)
	// Asset turns an image reference (an /assets/ URL, a remote URL, or on a
	// local server a file path) into a stored /assets/ URL and its pixel size.
	Asset(ref string) (url string, width, height int, err error)
	// Render draws a board as an encoded image.
	Render(b *Board, opts RenderOptions) ([]byte, error)
	// Fonts lists the families available to text.
	Fonts() []string
	// RemoveBackground cuts the subject out of a picture (rembg with a
	// BiRefNet model) and returns the cut-out, same pixel size, transparent
	// where the background was.
	RemoveBackground(ref, model string) (url string, width, height int, err error)
}

// TextSize is a laid-out text layer's box.
type TextSize struct {
	Height float64 `json:"height"`
	Width  float64 `json:"width"`
	Lines  int     `json:"lines"`
}

// RenderOptions shape an export.
type RenderOptions struct {
	Scale       float64  `json:"scale"`
	Format      string   `json:"format"` // png | jpeg | webp
	Quality     float64  `json:"quality"`
	Transparent bool     `json:"transparent"`
	IDs         []string `json:"ids,omitempty"`
}

// Error is a command failure worded for whoever sent it (often an AI agent):
// it says what exists, so the next attempt can be right.
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

func errf(format string, args ...any) error { return &Error{fmt.Sprintf(format, args...)} }

// Result is what applying a command produced.
type Result struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
	// Changed is true when the document differs from before (save it).
	Changed bool `json:"-"`
}

// Engine holds one project and applies commands to it. Every edit — human or
// AI — comes through Apply, one at a time, with a shared undo history.
type Engine struct {
	mu   sync.Mutex
	p    *Project
	svc  Services
	hist history
	// measured remembers which layout each text layer was measured for.
	measured map[string]string
}

// NewEngine wraps a project.
func NewEngine(p *Project, svc Services) *Engine {
	e := &Engine{p: p, svc: svc, hist: history{limit: 50}, measured: map[string]string{}}
	// The heights a document arrives with were measured when it was saved;
	// trust them until the layer changes.
	e.trustStoredHeights()
	return e
}

// Project returns a copy of the current document.
func (e *Engine) Project() *Project {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.p.Clone()
}

// Snapshot is the current document as stored.
func (e *Engine) Snapshot() []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.p.JSON()
}

// CanUndo / CanRedo report the history (for the editor's buttons).
func (e *Engine) CanUndo() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.hist.undo) > 0
}

func (e *Engine) CanRedo() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.hist.redo) > 0
}

// Replace swaps the whole document (an import), keeping it undoable.
func (e *Engine) Replace(p *Project) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hist.push(e.p.JSON())
	e.p = p
	e.measured = map[string]string{}
	e.trustStoredHeights()
	_ = e.measure()
}

func (e *Engine) trustStoredHeights() {
	var walk func([]*Layer)
	walk = func(ls []*Layer) {
		for _, l := range ls {
			if l.Type == "text" && l.Height > 0 {
				e.measured[l.ID] = layoutKey(l)
			} else if l.Type == "group" {
				walk(l.Children)
			}
		}
	}
	for _, b := range e.p.Boards {
		walk(b.Layers)
	}
}

// EnsureMeasured measures any text layer whose stored height may be stale
// (a document from elsewhere). It reports whether anything changed.
func (e *Engine) EnsureMeasured() (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	before := e.p.JSON()
	err := e.measure()
	return !bytes.Equal(before, e.p.JSON()), err
}

// Apply runs one command (or a batch) and reports the outcome.
func (e *Engine) Apply(raw []byte) Result {
	e.mu.Lock()
	defer e.mu.Unlock()
	cmd, err := decode(raw)
	if err != nil {
		return Result{Error: err.Error()}
	}
	before := e.p.JSON()
	data, err := e.apply(cmd, false)
	after := e.p.JSON()
	if err != nil {
		return Result{Error: err.Error(), Changed: !bytes.Equal(before, after)}
	}
	return Result{OK: true, Data: data, Changed: !bytes.Equal(before, after)}
}

func decode(raw []byte) (map[string]any, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			raw = []byte(s)
		}
	}
	if len(raw) > 0 && raw[0] == '[' {
		var list []any
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, errf("commands are not valid JSON")
		}
		return map[string]any{"type": "batch", "commands": list}, nil
	}
	var cmd map[string]any
	if err := json.Unmarshal(raw, &cmd); err != nil || cmd == nil {
		return nil, errf(`a command must be a JSON object with a "type" field`)
	}
	return cmd, nil
}

func (e *Engine) apply(cmd map[string]any, nested bool) (any, error) {
	typ := strings.TrimSpace(str(cmd["type"]))
	if typ == "" {
		return nil, errf(`a command needs a "type" field — see ` + "`davinci schema`")
	}
	// Any command may name the board it is for: bring that board up first.
	if ref, ok := cmd["board"]; ok && ref != nil && str(ref) != "" {
		b, _ := e.p.FindBoard(str(ref))
		if b == nil {
			return nil, errf(`no board "%s" — boards: %s`, str(ref), e.boardList())
		}
		e.p.Active = b.ID
		delete(cmd, "board")
	}

	if typ == "batch" {
		if err := checkParams(cmd, []string{"commands"}, "batch"); err != nil {
			return nil, err
		}
		list, _ := cmd["commands"].([]any)
		if len(list) == 0 {
			return nil, errf("batch needs a non-empty commands array")
		}
		before := e.p.JSON()
		out := make([]any, 0, len(list))
		for i, sub := range list {
			m, ok := sub.(map[string]any)
			if !ok {
				e.restore(before)
				return nil, errf("commands[%d] is not an object", i)
			}
			data, err := e.apply(m, true)
			if err != nil {
				// All or nothing: the caller fixes the failing command and resends.
				e.restore(before)
				return nil, err
			}
			out = append(out, data)
		}
		if !nested {
			e.hist.commit(before, e.p.JSON())
		}
		return out, nil
	}

	spec := registry[typ]
	if spec == nil {
		return nil, errf("unknown command \"%s\" — %d commands exist, run `davinci schema` to list them", typ, len(registry))
	}
	names := make([]string, len(spec.Params))
	for i, p := range spec.Params {
		names[i] = p.Name
	}
	if err := checkParams(cmd, names, typ); err != nil {
		return nil, err
	}
	c := &Ctx{e: e, P: e.p, B: e.p.ActiveBoard()}
	if spec.OwnHistory {
		if nested {
			return nil, errf("%s cannot run inside a batch", typ)
		}
		return spec.Run(c, cmd)
	}
	if !spec.Mutates || nested {
		data, err := spec.Run(c, cmd)
		if err == nil && spec.Mutates {
			err = e.measure()
		}
		return data, err
	}
	before := e.p.JSON()
	data, err := spec.Run(c, cmd)
	if err == nil {
		err = e.measure()
	}
	if err != nil {
		e.restore(before)
		return nil, err
	}
	e.hist.commit(before, e.p.JSON())
	return data, nil
}

func (e *Engine) restore(snapshot []byte) {
	if p, err := Parse(snapshot); err == nil {
		e.p = p
	}
}

func (e *Engine) boardList() string {
	parts := make([]string, len(e.p.Boards))
	for i, b := range e.p.Boards {
		parts[i] = fmt.Sprintf("%d:%s[%s]", i+1, b.Name, b.ID)
	}
	return strings.Join(parts, ", ")
}

// checkParams refuses parameters a command does not declare: a guessed name
// ("scale" for "multiplier") would otherwise be silently ignored.
func checkParams(cmd map[string]any, allowed []string, what string) error {
	ok := map[string]bool{"type": true}
	for _, a := range allowed {
		ok[a] = true
	}
	var extra []string
	for k := range cmd {
		if !ok[k] {
			extra = append(extra, `"`+k+`"`)
		}
	}
	if len(extra) == 0 {
		return nil
	}
	sort.Strings(extra)
	list := "takes no parameters"
	if len(allowed) > 0 {
		list = "takes " + strings.Join(allowed, ", ")
	}
	return errf("%s: unknown parameter %s — this command %s; run `davinci schema` for the full list", what, strings.Join(extra, ", "), list)
}

// --- text measurement ---------------------------------------------------------

// layoutKey is what a text layer's height depends on.
func layoutKey(l *Layer) string {
	st := l.Style
	b, _ := json.Marshal([]any{l.TextOf(), st["fontFamily"], st["fontSize"], st["fontWeight"], st["fontStyle"], st["lineHeight"], st["charSpacing"], st["stretch"], l.Width})
	return string(b)
}

// measure re-measures every text layer (in every board, groups included)
// whose layout changed since it was last measured.
func (e *Engine) measure() error {
	var stale []*Layer
	var walk func([]*Layer)
	walk = func(ls []*Layer) {
		for _, l := range ls {
			if l.Type == "text" {
				if e.measured[l.ID] != layoutKey(l) {
					stale = append(stale, l)
				}
			} else if l.Type == "group" {
				walk(l.Children)
			}
		}
	}
	for _, b := range e.p.Boards {
		walk(b.Layers)
	}
	if len(stale) == 0 || e.svc == nil {
		return nil
	}
	sizes, err := e.svc.Measure(stale)
	if err != nil {
		// Without a renderer the heights stay estimates; nothing is lost.
		for _, l := range stale {
			l.Height = roundTo(estimateHeight(l), 1)
		}
		return nil
	}
	for _, l := range stale {
		if s, ok := sizes[l.ID]; ok {
			l.Height = roundTo(s.Height, 1)
			e.measured[l.ID] = layoutKey(l)
		}
	}
	return nil
}

// estimateHeight follows the layout's height rule with explicit lines only.
func estimateHeight(l *Layer) float64 {
	fs := num(l.S("fontSize"), 40)
	lh := num(l.S("lineHeight"), 1.16)
	n := float64(strings.Count(l.TextOf(), "\n") + 1)
	return (n-1)*fs*1.13*lh + fs*1.13
}

// --- history ------------------------------------------------------------------

// history keeps whole-project snapshots: one step can span boards, and undo
// brings back the board it happened on.
type history struct {
	undo, redo [][]byte
	limit      int
}

func (h *history) commit(before, after []byte) {
	if bytes.Equal(before, after) {
		return
	}
	h.push(before)
}

func (h *history) push(before []byte) {
	h.undo = append(h.undo, before)
	if len(h.undo) > h.limit {
		h.undo = h.undo[1:]
	}
	h.redo = nil
}

func (e *Engine) step(undo bool) (any, error) {
	from, to := &e.hist.undo, &e.hist.redo
	if !undo {
		from, to = to, from
	}
	if len(*from) == 0 {
		return map[string]any{"changed": false}, nil
	}
	target := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	*to = append(*to, e.p.JSON())
	if len(*to) > e.hist.limit {
		*to = (*to)[1:]
	}
	e.restore(target)
	return map[string]any{"changed": true}, nil
}
