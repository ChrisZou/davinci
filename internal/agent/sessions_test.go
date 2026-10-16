package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func sessionHelper() {
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	seq := 0
	for {
		var msg struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if decoder.Decode(&msg) != nil {
			os.Exit(0)
		}
		f, _ := os.OpenFile(os.Getenv("DAVINCI_TEST_CAPTURE"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		_ = json.NewEncoder(f).Encode(msg)
		_ = f.Close()
		reply := func(v any) { _ = encoder.Encode(map[string]any{"id": msg.ID, "result": v}) }
		event := func(method string, params any) {
			_ = encoder.Encode(map[string]any{"method": method, "params": params})
		}
		switch msg.Method {
		case "initialized":
		case "config/read":
			reply(map[string]any{"config": map[string]any{"mcp_servers": map[string]any{"test-tool": map[string]any{}}}})
		case "thread/start":
			seq++
			reply(map[string]any{"thread": map[string]any{"id": fmt.Sprintf("thread-%d", seq)}})
		case "thread/resume":
			reply(map[string]any{"thread": map[string]any{"id": msg.Params["threadId"]}})
		case "turn/start":
			prompt := msg.Params["input"].([]any)[0].(map[string]any)["text"].(string)
			if prompt == "exit" {
				os.Exit(9)
			}
			thread := msg.Params["threadId"]
			reply(map[string]any{"turn": map[string]any{"id": "turn-1"}})
			if prompt == "wait" {
				continue
			}
			raw, _ := json.Marshal(Response{Summary: prompt, Commands: `[{"type":"setBackground","color":"red"}]`})
			if prompt == "bad" {
				raw = []byte("not json")
			}
			event("item/completed", map[string]any{"threadId": thread, "turnId": "turn-1", "item": map[string]any{"type": "agentMessage", "text": string(raw)}})
			event("turn/completed", map[string]any{"threadId": thread, "turn": map[string]any{"id": "turn-1", "status": "completed", "items": []any{}}})
		case "turn/interrupt":
			reply(map[string]any{})
			event("turn/completed", map[string]any{"threadId": msg.Params["threadId"], "turn": map[string]any{"id": msg.Params["turnId"], "status": "interrupted", "items": []any{}}})
		default:
			reply(map[string]any{})
		}
	}
}

func sessionFixture(t *testing.T) (*Sessions, string, func() *Sessions) {
	t.Helper()
	capture := fakeAgent(t, "codex", "session")
	var mu sync.Mutex
	saved := map[string]string{}
	dir := filepath.Join(t.TempDir(), "sessions")
	create := func() *Sessions {
		s := NewSessions(dir, func(id string) (string, error) { mu.Lock(); defer mu.Unlock(); return saved[id], nil }, func(id, thread string) error { mu.Lock(); defer mu.Unlock(); saved[id] = thread; return nil })
		t.Cleanup(s.Close)
		return s
	}
	return create(), capture, create
}

func capturedCalls(t *testing.T, path string) []struct {
	Method string
	Params map[string]any
} {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var calls []struct {
		Method string
		Params map[string]any
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var call struct {
			Method string
			Params map[string]any
		}
		if err = json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
	return calls
}

func TestSessionsWarmReuseIsolationAndResume(t *testing.T) {
	s, capture, create := sessionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a := Request{ProjectID: "A", Instructions: "rules", Context: "project A", Prompt: "first", Image: []byte("png")}
	id, err := s.Warm(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.Warm(ctx, a)
	if err != nil || same != id {
		t.Fatal(same, err)
	}
	for _, call := range capturedCalls(t, capture) {
		if call.Method == "turn/start" {
			t.Fatal("warming used model inference")
		}
	}
	b := a
	b.ProjectID = "B"
	b.Context = "project B"
	other, err := s.Warm(ctx, b)
	if err != nil || other == id {
		t.Fatal("projects share a thread", err)
	}
	for _, prompt := range []string{"first", "second"} {
		a.Prompt = prompt
		r, err := s.Run(ctx, a)
		if err != nil || r.Summary != prompt {
			t.Fatal(r, err)
		}
	}
	counts := map[string]int{}
	for _, call := range capturedCalls(t, capture) {
		counts[call.Method]++
		if call.Method == "thread/start" {
			cfg := call.Params["config"].(map[string]any)
			if cfg["mcp_servers.test-tool.enabled"] != false || cfg["features.shell_tool"] != false {
				t.Fatal("tools not disabled")
			}
		}
		if call.Method == "turn/start" && call.Params["threadId"] != id {
			t.Fatal("wrong project")
		}
	}
	if counts["initialize"] != 1 || counts["thread/start"] != 2 || counts["thread/inject_items"] != 2 || counts["turn/start"] != 2 {
		t.Fatal(counts)
	}
	s.Close()
	s2 := create()
	resumed, err := s2.Warm(ctx, a)
	if err != nil || resumed != id {
		t.Fatal("did not restore thread", err)
	}
	calls := capturedCalls(t, capture)
	found := false
	for _, c := range calls {
		if c.Method == "thread/resume" && c.Params["threadId"] == id {
			found = true
		}
	}
	if !found {
		t.Fatal("resume RPC missing")
	}
}

func TestSessionsCancelAndRecoverWithoutReplay(t *testing.T) {
	s, capture, _ := sessionFixture(t)
	req := Request{ProjectID: "A", Context: "project A", Prompt: "wait"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.Warm(ctx, req); err != nil {
		t.Fatal(err)
	}
	turnCtx, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	if _, err := s.Run(turnCtx, req); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	req.Prompt = "after cancel"
	if _, err := s.Run(ctx, req); err != nil {
		t.Fatal(err)
	}
	req.Prompt = "exit"
	if _, err := s.Run(ctx, req); err == nil {
		t.Fatal("process exit accepted")
	}
	req.Prompt = "recovered"
	if _, err := s.Run(ctx, req); err != nil {
		t.Fatal(err)
	}
	req.Prompt = "bad"
	if _, err := s.Run(ctx, req); err == nil {
		t.Fatal("bad JSON accepted")
	}
	exitTurns, interrupts := 0, 0
	for _, c := range capturedCalls(t, capture) {
		if c.Method == "turn/interrupt" {
			interrupts++
		}
		if c.Method == "turn/start" && c.Params["input"].([]any)[0].(map[string]any)["text"] == "exit" {
			exitTurns++
		}
	}
	if exitTurns != 1 || interrupts != 1 {
		t.Fatal(exitTurns, interrupts)
	}
}

func TestSessionsConcurrentProjects(t *testing.T) {
	s, _, _ := sessionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, id := range []string{"A", "B"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Run(ctx, Request{ProjectID: id, Context: id, Prompt: id})
			if err != nil {
				t.Error(err)
			} else if r.Summary != id {
				t.Error("mixed project response")
			}
		}()
	}
	wg.Wait()
}
