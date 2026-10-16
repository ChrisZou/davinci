package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// This subprocess speaks the real CLIs' stdin/stdout contracts without a model
// account, and exercises cancellation through the normal process launcher.
func TestAgentHelper(t *testing.T) {
	mode := os.Getenv("DAVINCI_TEST_HELPER")
	if mode == "" {
		return
	}
	if mode == "session" {
		sessionHelper()
		return
	}
	input, _ := io.ReadAll(os.Stdin)
	if p := os.Getenv("DAVINCI_TEST_CAPTURE"); p != "" {
		os.WriteFile(p, input, 0600)
	}
	if mode == "wait" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	if mode == "fail" {
		os.Stderr.WriteString("secret-test-token")
		os.Exit(9)
	}
	if mode == "login" {
		os.Stdout.WriteString(`{"type":"result","is_error":true,"result":"Not logged in · Please run /login"}`)
		os.Exit(1)
	}
	out := Response{Summary: "已修改", Commands: `[{"type":"setBackground","color":"red"}]`}
	raw, _ := json.Marshal(out)
	if mode == "bad" {
		raw = []byte("not JSON")
	}
	if mode == "claude" {
		raw, _ = json.Marshal(map[string]any{"type": "result", "structured_output": out, "is_error": false})
		raw = append([]byte("{\"type\":\"system\"}\n{\"type\":\"assistant\"}\n"), raw...)
	}
	for i, a := range os.Args {
		if a == "--output-last-message" && i+1 < len(os.Args) {
			os.WriteFile(os.Args[i+1], raw, 0600)
		}
	}
	os.Stdout.Write(raw)
	os.Exit(0)
}

func fakeAgent(t *testing.T, provider, mode string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable fixture")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, provider)
	content := "#!/bin/sh\nexec '" + strings.ReplaceAll(binary, "'", "'\\''") + "' -test.run=TestAgentHelper -- \"$@\"\n"
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(dir, "input.json")
	t.Setenv("DAVINCI_"+strings.ToUpper(provider)+"_BIN", script)
	t.Setenv("DAVINCI_TEST_HELPER", mode)
	t.Setenv("DAVINCI_TEST_CAPTURE", capture)
	return capture
}

func TestRunnerContracts(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			capture := fakeAgent(t, provider, provider)
			prompt := "把标题改成黄色；$(touch /tmp/should-not-run)\n保留其他内容"
			r, err := Run(context.Background(), Request{Provider: provider, Prompt: prompt, Image: []byte("image")})
			if err != nil {
				t.Fatal(err)
			}
			if r.Summary != "已修改" {
				t.Fatal(r)
			}
			input, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			if provider == "codex" {
				if string(input) != prompt {
					t.Fatal("stdin prompt changed")
				}
			} else {
				var msg struct {
					Message struct {
						Content []struct {
							Type   string `json:"type"`
							Text   string `json:"text"`
							Source struct {
								Data string `json:"data"`
							} `json:"source"`
						} `json:"content"`
					} `json:"message"`
				}
				if json.Unmarshal(input, &msg) != nil || len(msg.Message.Content) != 2 || msg.Message.Content[0].Text != prompt || msg.Message.Content[1].Source.Data != "aW1hZ2U=" {
					t.Fatalf("bad image message: %s", input)
				}
			}
		})
	}
}

func TestRunnerCancellationAndFailure(t *testing.T) {
	t.Run("login", func(t *testing.T) {
		fakeAgent(t, "claude", "login")
		_, err := Run(context.Background(), Request{Provider: "claude", Prompt: "test"})
		if err == nil || !strings.Contains(err.Error(), "尚未登录") {
			t.Fatalf("login error: %v", err)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		fakeAgent(t, "codex", "wait")
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, err := Run(ctx, Request{Provider: "codex", Prompt: "test"})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})
	for _, mode := range []string{"fail", "bad"} {
		t.Run(mode, func(t *testing.T) {
			fakeAgent(t, "codex", mode)
			_, err := Run(context.Background(), Request{Provider: "codex", Prompt: "test"})
			if err == nil || strings.Contains(err.Error(), "secret-test-token") {
				t.Fatalf("bad error: %v", err)
			}
		})
	}
}
