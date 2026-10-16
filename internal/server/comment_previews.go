package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"davinci/internal/doc"
)

const commentPreviewBudget = 32 << 20

type previewKey struct {
	content    [32]byte
	generation uint64
}

type previewEntry struct {
	done  chan struct{}
	image []byte
	err   error
	used  uint64
}

// Content-addressed previews cannot accidentally reuse an older document.
// Pending renders are shared by prewarming and a simultaneous comment.
type commentPreviews struct {
	mu         sync.Mutex
	entries    map[previewKey]*previewEntry
	timers     map[string]*time.Timer
	render     func(*doc.Board) ([]byte, error)
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	tick       uint64
	bytes      int
	generation uint64
}

func newCommentPreviews(render func(*doc.Board) ([]byte, error)) *commentPreviews {
	ctx, cancel := context.WithCancel(context.Background())
	return &commentPreviews{entries: map[previewKey]*previewEntry{}, timers: map[string]*time.Timer{}, render: render, ctx: ctx, cancel: cancel}
}

func (p *commentPreviews) get(ctx context.Context, b *doc.Board) ([]byte, bool, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return nil, false, err
	}
	digest := sha256.Sum256(raw)
	p.mu.Lock()
	key := previewKey{digest, p.generation}
	if p.ctx.Err() != nil {
		p.mu.Unlock()
		return nil, false, context.Canceled
	}
	p.tick++
	entry, hit := p.entries[key]
	if !hit {
		// The renderer must see exactly the snapshot used as the cache key, even if
		// the caller retains and later changes its Board value.
		var snapshot doc.Board
		if err = json.Unmarshal(raw, &snapshot); err != nil {
			p.mu.Unlock()
			return nil, false, err
		}
		entry = &previewEntry{done: make(chan struct{})}
		p.entries[key] = entry
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			img, renderErr := p.render(&snapshot)
			if renderErr == nil && len(img) == 0 {
				renderErr = errors.New("画板预览为空")
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			entry.image, entry.err = img, renderErr
			close(entry.done)
			if p.entries[key] != entry {
				return
			}
			if renderErr != nil || len(img) > commentPreviewBudget {
				delete(p.entries, key)
				return
			}
			p.bytes += len(img)
			p.evict()
		}()
	}
	entry.used = p.tick
	p.mu.Unlock()
	select {
	case <-entry.done:
		return entry.image, hit, entry.err
	case <-ctx.Done():
		return nil, hit, ctx.Err()
	case <-p.ctx.Done():
		return nil, hit, context.Canceled
	}
}

// Called with the mutex held. Running jobs remain available to their waiters.
func (p *commentPreviews) evict() {
	for p.bytes > commentPreviewBudget || len(p.entries) > 16 {
		var oldest previewKey
		var found *previewEntry
		for k, e := range p.entries {
			select {
			case <-e.done:
				if found == nil || e.used < found.used {
					oldest, found = k, e
				}
			default:
			}
		}
		if found == nil {
			return
		}
		p.bytes -= len(found.image)
		delete(p.entries, oldest)
	}
}

func (p *commentPreviews) schedule(project string, b *doc.Board) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx.Err() != nil {
		return
	}
	if old := p.timers[project]; old != nil {
		old.Stop()
	}
	var timer *time.Timer
	timer = time.AfterFunc(250*time.Millisecond, func() {
		p.mu.Lock()
		if p.ctx.Err() != nil || p.timers[project] != timer {
			p.mu.Unlock()
			return
		}
		delete(p.timers, project)
		p.mu.Unlock()
		_, _, _ = p.get(p.ctx, b)
	})
	p.timers[project] = timer
}

func (p *commentPreviews) close() {
	p.mu.Lock()
	p.cancel()
	for _, t := range p.timers {
		t.Stop()
	}
	p.timers = map[string]*time.Timer{}
	p.mu.Unlock()
	p.wg.Wait()
}

func (s *Server) scheduleCommentPreview(projectID string, snapshot []byte) {
	if s.assets.remote || s.hub.Connected(projectID) == 0 {
		return
	}
	p, err := doc.Parse(snapshot)
	if err != nil {
		return
	}
	s.comments.previews.schedule(projectID, p.ActiveBoard())
}

func (p *commentPreviews) invalidate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.generation++
	p.entries = map[previewKey]*previewEntry{}
	p.bytes = 0
}
