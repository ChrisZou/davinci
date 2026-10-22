package agent

import (
	"context"
	"os"
	"testing"
	"time"
)

// Opt-in integration check against the user's installed, signed-in Codex.
// It uses two short model turns; ordinary tests never contact a model.
func TestLiveCodexSession(t *testing.T) {
	if os.Getenv("DAVINCI_TEST_LIVE_CODEX") != "1" {
		t.Skip("requires explicit live Codex opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	saved := ""
	create := func() *Sessions {
		return NewSessions(t.TempDir(), func(string, string) (string, error) { return saved, nil }, func(_, id, _ string) error { saved = id; return nil })
	}
	s := create()
	defer s.Close()
	req := Request{ProjectID: "davinci-session-integration", Instructions: "You are testing a graphics editor session. Do not use tools. Return only the requested structured JSON. commands must always be the string [].", Context: "Project context: the project code is PAPAYA-47. This is context only, not an edit request.", Prompt: "What is the project code? Put only the code in summary; commands must be []."}
	start := time.Now()
	id, err := s.Warm(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("warm", time.Since(start).Round(time.Millisecond))
	// Even a session with no inference yet must survive a process restart.
	s.Close()
	s = create()
	defer s.Close()
	resumed, err := s.Warm(ctx, req)
	if err != nil || resumed != id {
		t.Fatal("warm-only resume", resumed, id, err)
	}
	start = time.Now()
	out, err := s.Run(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Summary != "PAPAYA-47" {
		t.Fatal("preloaded context missing", out)
	}
	t.Log("first turn", time.Since(start).Round(time.Millisecond))
	req.Prompt = "Repeat the code from your previous answer. Put only the code in summary; commands must be []."
	start = time.Now()
	out, err = s.Run(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Summary != "PAPAYA-47" {
		t.Fatal("conversation not retained", out)
	}
	t.Log("second turn", time.Since(start).Round(time.Millisecond))
	if err = s.process.call(ctx, "thread/archive", map[string]any{"threadId": id}, nil); err != nil {
		t.Log("archive", err)
	}
}
