package server

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"davinci/internal/agent"
	"davinci/internal/doc"
	"github.com/gorilla/websocket"
)

func TestCommentPreviewSharesRenderAndMatchesContent(t *testing.T) {
	entered, release := make(chan struct{}, 2), make(chan struct{})
	var calls atomic.Int32
	p := newCommentPreviews(func(b *doc.Board) ([]byte, error) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return []byte(b.Canvas.Background), nil
	})
	defer p.close()
	b := doc.Blank(100, 100).ActiveBoard()
	b.Canvas.Background = "red"
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, _, err := p.get(ctx, b); result <- err }()
	<-entered
	// A cancelled waiter must not cancel the shared render.
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	img, hit, err := p.get(context.Background(), b)
	if err != nil || !hit || string(img) != "red" || calls.Load() != 1 {
		t.Fatal(string(img), hit, err, calls.Load())
	}
	b.Canvas.Background = "blue"
	img, hit, err = p.get(context.Background(), b)
	if err != nil || hit || string(img) != "blue" || calls.Load() != 2 {
		t.Fatal("stale preview", string(img), hit, err)
	}
	b.Canvas.Background = "red"
	if _, hit, err = p.get(context.Background(), b); err != nil || !hit {
		t.Fatal("undo did not reuse exact preview", err)
	}
}

func TestCommentPreviewFailureRetryInvalidationAndBudget(t *testing.T) {
	var calls int
	p := newCommentPreviews(func(*doc.Board) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("render failed")
		}
		return make([]byte, 3<<20), nil
	})
	defer p.close()
	b := doc.Blank(100, 100).ActiveBoard()
	if _, _, err := p.get(context.Background(), b); err == nil {
		t.Fatal("failure hidden")
	}
	if _, hit, err := p.get(context.Background(), b); err != nil || hit {
		t.Fatal("failed render cached", err)
	}
	p.invalidate()
	if _, hit, err := p.get(context.Background(), b); err != nil || hit {
		t.Fatal("font invalidation ignored", err)
	}
	for i := 0; i < 20; i++ {
		b.Canvas.Width = float64(200 + i)
		if _, _, err := p.get(context.Background(), b); err != nil {
			t.Fatal(err)
		}
	}
	p.mu.Lock()
	size, count := p.bytes, len(p.entries)
	p.mu.Unlock()
	if size > commentPreviewBudget || count > 16 {
		t.Fatal("cache unbounded", size, count)
	}
}

func TestCommentPreviewWarmOnOpenAndEdit(t *testing.T) {
	h, id, e, c := setupComment(t)
	rendered := make(chan string, 10)
	h.srv.comments.preview = func(b *doc.Board) ([]byte, error) {
		rendered <- b.Canvas.Background
		return []byte(b.Canvas.Background), nil
	}
	// Opening the websocket is what the editor does for either AI provider.
	ws, _, err := websocket.DefaultDialer.Dial(strings.Replace(h.srv.Addr(), "http:", "ws:", 1)+"/ws?project="+id, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	waitRender := func(want string) {
		t.Helper()
		select {
		case got := <-rendered:
			if got != want {
				t.Fatalf("preview %s != %s", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("preview not warmed")
		}
	}
	waitRender(e.Project().ActiveBoard().Canvas.Background)
	if _, err = h.srv.Exec(id, []byte(`{"type":"setBackground","color":"#123456"}`)); err != nil {
		t.Fatal(err)
	}
	waitRender("#123456")
	h.srv.comments.run = func(_ context.Context, r agent.Request) (*agent.Response, error) {
		if string(r.Image) != "#123456" {
			t.Error("stale image sent to AI")
		}
		return &agent.Response{Summary: "无需修改", Commands: `[]`}, nil
	}
	code, raw := h.do("POST", "/api/projects/"+id+"/comments", c)
	if code != 202 {
		t.Fatalf("%d %s", code, raw)
	}
	waitComment(t, h, id, "needs_info")
	select {
	case <-rendered:
		t.Fatal("comment rendered again despite warmed cache")
	default:
	}
}

func TestCommentPreviewDebouncesAndStops(t *testing.T) {
	rendered := make(chan string, 2)
	p := newCommentPreviews(func(b *doc.Board) ([]byte, error) { rendered <- b.Canvas.Background; return []byte("png"), nil })
	for _, color := range []string{"red", "blue", "green"} {
		b := doc.Blank(100, 100).ActiveBoard()
		b.Canvas.Background = color
		p.schedule("project", b)
	}
	select {
	case got := <-rendered:
		if got != "green" {
			t.Fatal(got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("preview not warmed")
	}
	p.schedule("project", doc.Blank(200, 200).ActiveBoard())
	p.close()
	if _, _, err := p.get(context.Background(), doc.Blank(300, 300).ActiveBoard()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-rendered:
		t.Fatal("extra render after close")
	default:
	}
}
