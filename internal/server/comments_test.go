package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"davinci/internal/agent"
	"davinci/internal/doc"
	"github.com/gorilla/websocket"
)

func setupComment(t *testing.T) (*harness, string, *doc.Engine, Comment) {
	t.Helper()
	h := start(t)
	id := h.project("评论测试")
	e, err := h.srv.engine(id)
	if err != nil {
		t.Fatal(err)
	}
	b := e.Project().ActiveBoard()
	return h, id, e, Comment{ID: "comment_test", ProjectID: id, BoardID: b.ID, Provider: "codex", Text: "把背景改成黄色", LayerIDs: []string{}, CreatedAt: time.Now().UTC()}
}

func TestCommentBatchUndoAndConflict(t *testing.T) {
	h, _, e, c := setupComment(t)
	before := e.Snapshot()
	r := &agent.Response{Summary: "改成黄色并调整尺寸", Commands: `[{"type":"setBackground","color":"#ffff00"},{"type":"setCanvasSize","width":900,"height":600}]`}
	if err := h.srv.applyComment(&c, r, before); err != nil {
		t.Fatal(err)
	}
	if c.Status != "done" || e.Project().ActiveBoard().Canvas.Background != "#ffff00" {
		t.Fatal("edit not applied")
	}
	if res := e.Apply([]byte(`{"type":"undo"}`)); !res.OK {
		t.Fatal(res.Error)
	}
	if !bytes.Equal(e.Snapshot(), before) {
		t.Fatal("AI batch did not undo in one step")
	}
	e.Apply([]byte(`{"type":"setBackground","color":"#112233"}`))
	if err := h.srv.applyComment(&c, r, before); err == nil || !strings.Contains(err.Error(), "已发生变化") {
		t.Fatalf("stale result: %v", err)
	}
	if e.Project().ActiveBoard().Canvas.Background != "#112233" {
		t.Fatal("human edit overwritten")
	}
}

func TestCommentFailureRestoresBoardAndNoPartialEdits(t *testing.T) {
	h, _, e, c := setupComment(t)
	e.Apply([]byte(`{"type":"addBoard","name":"second"}`))
	before := e.Snapshot()
	r := &agent.Response{Summary: "bad", Commands: `[{"type":"setBackground","color":"#ffff00"},{"type":"moveLayer","id":"missing","x":1,"y":2}]`}
	if err := h.srv.applyComment(&c, r, before); err == nil {
		t.Fatal("expected invalid layer error")
	}
	if !bytes.Equal(e.Snapshot(), before) {
		t.Fatal("failed batch changed document or active board")
	}
}

func TestCommentRejectsUnsupportedCommands(t *testing.T) {
	h, _, e, c := setupComment(t)
	before := e.Snapshot()
	for _, commands := range []string{`[{"type":"undo"}]`, `[{"type":"batch","commands":[]}]`, `[{"type":"setBackground","board":"other","color":"red"}]`, `[{"type":"addImage","path":"/etc/passwd"}]`, `[{"type":"replaceImage","id":"x","url":"https://example.com/a.png"}]`, `null`, `{}`} {
		if err := h.srv.applyComment(&c, &agent.Response{Summary: "test", Commands: commands}, before); err == nil {
			t.Errorf("accepted %s", commands)
		}
		if !bytes.Equal(before, e.Snapshot()) {
			t.Fatal("invalid command changed the document")
		}
	}
	if err := h.srv.applyComment(&c, &agent.Response{Summary: "请先选中标题", Commands: `[]`}, before); err != nil || c.Status != "needs_info" {
		t.Fatalf("clarification: %v %+v", err, c)
	}
}

func waitComment(t *testing.T, h *harness, id string, status string) Comment {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		list, err := h.srv.listComments(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) > 0 && list[len(list)-1].Status == status {
			return list[len(list)-1]
		}
		time.Sleep(10 * time.Millisecond)
	}
	list, _ := h.srv.listComments(id)
	t.Fatalf("did not reach %s: %+v", status, list)
	return Comment{}
}

func TestCommentHTTPExecutionAndPersistence(t *testing.T) {
	h, id, e, c := setupComment(t)
	h.srv.comments.preview = func(*doc.Board) ([]byte, error) { return []byte("preview"), nil }
	h.srv.comments.run = func(ctx context.Context, r agent.Request) (*agent.Response, error) {
		if !strings.Contains(r.Prompt, c.Text) || string(r.Image) != "preview" {
			return nil, errors.New("context missing")
		}
		return &agent.Response{Summary: "已改成黄色", Commands: `[{"type":"setBackground","color":"#ffff00"}]`}, nil
	}
	code, raw := h.do("POST", "/api/projects/"+id+"/comments", c)
	if code != 202 {
		t.Fatalf("%d: %s", code, raw)
	}
	done := waitComment(t, h, id, "done")
	if done.Text != c.Text || done.Reply != "已改成黄色" || e.Project().ActiveBoard().Canvas.Background != "#ffff00" {
		t.Fatalf("bad result: %+v", done)
	}
	code, raw = h.do("GET", "/api/projects/"+id+"/comments", nil)
	if code != 200 || !bytes.Contains(raw, []byte(done.ID)) {
		t.Fatalf("history: %d %s", code, raw)
	}
	h.srv.comments.wg.Wait()
	// Simulate a saved interrupted request during the next server startup.
	done.Status = "running"
	if err := h.srv.saveComment(done); err != nil {
		t.Fatal(err)
	}
	if err := h.srv.initComments(); err != nil {
		t.Fatal(err)
	}
	waitComment(t, h, id, "failed")
}

func TestCommentCancelAndDuplicate(t *testing.T) {
	h, id, e, c := setupComment(t)
	before := e.Snapshot()
	entered := make(chan struct{})
	h.srv.comments.preview = func(*doc.Board) ([]byte, error) { return []byte("preview"), nil }
	h.srv.comments.run = func(ctx context.Context, r agent.Request) (*agent.Response, error) {
		close(entered)
		<-ctx.Done()
		return &agent.Response{Summary: "late", Commands: `[{"type":"setBackground","color":"red"}]`}, nil
	}
	code, raw := h.do("POST", "/api/projects/"+id+"/comments", c)
	if code != 202 {
		t.Fatalf("%d %s", code, raw)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("runner not entered")
	}
	if code, _ := h.do("POST", "/api/projects/"+id+"/comments", c); code != 409 {
		t.Fatalf("duplicate: %d", code)
	}
	var out struct {
		Comment Comment `json:"comment"`
	}
	json.Unmarshal(raw, &out)
	if code, raw := h.do("POST", "/api/projects/"+id+"/comments/"+out.Comment.ID+"/cancel", map[string]any{}); code != 200 {
		t.Fatalf("cancel: %d %s", code, raw)
	}
	h.srv.comments.wg.Wait()
	waitComment(t, h, id, "cancelled")
	if !bytes.Equal(before, e.Snapshot()) {
		t.Fatal("cancelled result applied")
	}
}

func TestCommentOriginAndInputGuards(t *testing.T) {
	h, id, _, c := setupComment(t)
	body, _ := json.Marshal(c)
	for _, origin := range []string{"https://evil.example", "null", "http://127.0.0.1:7789.evil.example"} {
		req, _ := http.NewRequest("POST", h.url("/api/projects/"+id+"/comments"), bytes.NewReader(body))
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		res, err := h.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatalf("origin %s: %d", origin, res.StatusCode)
		}
	}
	c.BoardID = "missing"
	if code, _ := h.do("POST", "/api/projects/"+id+"/comments", c); code != 400 {
		t.Fatal(code)
	}
	c.Text = " "
	if code, _ := h.do("POST", "/api/projects/"+id+"/comments", c); code != 400 {
		t.Fatal(code)
	}
	h.srv.assets.remote = true
	if code, _ := h.do("GET", "/api/ai/providers", nil); code != 403 {
		t.Fatal(code)
	}
}

func TestCommentAllowedCommandsExist(t *testing.T) {
	found := map[string]bool{}
	for _, c := range doc.Schema()["commands"].([]map[string]any) {
		found[str(c["type"])] = true
	}
	for name := range commentCommands {
		if !found[name] {
			t.Errorf("nonexistent command %s", name)
		}
	}
}

func TestCommentsThroughLocalDomain(t *testing.T) {
	h, id, _, _ := setupComment(t)
	for _, tc := range []struct {
		origin, host string
		status       int
	}{
		{"http://davinci.localhost", "davinci.localhost", http.StatusBadRequest},
		{"http://davinci.localhost:8880", "davinci.localhost:8880", http.StatusBadRequest},
		{"http://davinci-dev.localhost", "davinci-dev.localhost", http.StatusBadRequest},
		{"http://davinci-dev.localhost", "davinci.localhost", http.StatusForbidden},
		{"http://davinci-dev.localhost.evil.example", "davinci-dev.localhost.evil.example", http.StatusForbidden},
		{"http://davinci.localhost", "other.localhost", http.StatusForbidden},
		{"http://davinci.localhost.evil.example", "davinci.localhost.evil.example", http.StatusForbidden},
		{"http://evil.localhost", "evil.localhost", http.StatusForbidden},
	} {
		// An empty comment must reach input validation through the local proxy,
		// without launching a model. Other hosts must stop at the origin guard.
		req, _ := http.NewRequest("POST", h.url("/api/projects/"+id+"/comments"), strings.NewReader(`{}`))
		req.Host = tc.host
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Content-Type", "application/json")
		res, err := h.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Errorf("origin %s, host %s: got %d, want %d", tc.origin, tc.host, res.StatusCode, tc.status)
		}
	}
}

func TestConcurrentCommentResultsCannotOverwriteEachOther(t *testing.T) {
	h, id, e, c := setupComment(t)
	before := e.Snapshot()
	outcomes := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func() {
			copy := c
			outcomes <- h.srv.applyComment(&copy, &agent.Response{Summary: "黄色", Commands: `[{"type":"setBackground","color":"#ffff00"}]`}, before)
		}()
	}
	successes := 0
	for i := 0; i < 16; i++ {
		if <-outcomes == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("accepted %d competing results, want 1", successes)
	}
	saved, err := h.srv.store.GetProject(id)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved.Document, e.Snapshot()) {
		t.Fatal("stored document does not match live canvas")
	}
}

func TestWarmSessionPreloadsProjectWithoutEditing(t *testing.T) {
	h, id, e, _ := setupComment(t)
	before := e.Snapshot()
	calls := 0
	h.srv.comments.warm = func(ctx context.Context, req agent.Request) (string, error) {
		calls++
		if req.ProjectID != id || !strings.Contains(req.Context, "评论测试") || !strings.Contains(req.Context, e.Project().Active) || !strings.Contains(req.Instructions, "setTextStyle") {
			t.Error("missing project context or command instructions")
		}
		return "thread-test", nil
	}
	code, raw := h.do("POST", "/api/projects/"+id+"/ai/session", map[string]any{})
	if code != 200 || !bytes.Contains(raw, []byte("thread-test")) || calls != 1 {
		t.Fatalf("warm: %d %s", code, raw)
	}
	if !bytes.Equal(before, e.Snapshot()) {
		t.Fatal("warming edited the document")
	}
	comments, err := h.srv.listComments(id)
	if err != nil || len(comments) != 0 {
		t.Fatal("warming created a comment", err)
	}
	code, _ = h.do("POST", "/api/projects/missing/ai/session", map[string]any{})
	if code != 404 || calls != 1 {
		t.Fatal("missing project warmed", code)
	}
}

func TestCommentCompletionTravelsWithDocument(t *testing.T) {
	h, id, e, c := setupComment(t)
	ws, _, err := websocket.DefaultDialer.Dial(strings.Replace(h.srv.Addr(), "http:", "ws:", 1)+"/ws?project="+id, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	var env Envelope
	if err = ws.ReadJSON(&env); err != nil || env.Type != "hello" {
		t.Fatal(err, env.Type)
	}
	c.Status = "running"
	if err = h.srv.applyComment(&c, &agent.Response{Commands: `[{"type":"setBackground","color":"#ffff00"}]`}, e.Snapshot()); err != nil {
		t.Fatal(err)
	}
	if err = ws.ReadJSON(&env); err != nil {
		t.Fatal(err)
	}
	if env.Type != "doc" || env.Comment == nil || env.Comment.ID != c.ID || env.Comment.Status != "done" || env.Revision == 0 {
		t.Fatalf("completion missing from document: %+v", env)
	}
	p, err := doc.Parse(env.Document)
	if err != nil || p.ActiveBoard().Canvas.Background != "#ffff00" {
		t.Fatal("wrong document", err)
	}
	list, err := h.srv.listComments(id)
	if err != nil || len(list) != 1 || list[0].Status != "done" {
		t.Fatal("completion not persisted", list, err)
	}
	// Failure has no document update, but must still reach the composer immediately.
	c.ID = "failed_comment"
	h.srv.finishComment(&c, errors.New("test failure"))
	env = Envelope{}
	if err = ws.ReadJSON(&env); err != nil || env.Type != "comment" || env.Comment == nil || env.Comment.Status != "failed" || env.Document != nil {
		t.Fatal(err, env)
	}
}
