// Package agent adapts installed CLIs to a bounded, structured editing request.
// The model proposes commands; only the document engine can apply them.
package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const responseSchema = `{"type":"object","properties":{"summary":{"type":"string"},"commands":{"type":"string","description":"JSON encoded array of davinci commands; [] if unsupported or clarification needed"}},"required":["summary","commands"],"additionalProperties":false}`

type Provider struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

type Request struct {
	Provider, Prompt                 string
	ProjectID, Instructions, Context string
	// Session is the editor session (one page load) the conversation belongs to.
	Session string
	Image   []byte
}
type Response struct {
	Summary  string `json:"summary"`
	Commands string `json:"commands"`
}

func Providers() []Provider {
	out := []Provider{{ID: "codex", Name: "Codex"}, {ID: "claude", Name: "Claude Code"}}
	for i := range out {
		_, err := executable(out[i].ID)
		out[i].Available = err == nil
	}
	return out
}

// Finder-launched apps do not inherit a terminal's PATH. Check common native
// install locations too, without sourcing shell profiles or executing a shell.
func executable(name string) (string, error) {
	if name != "codex" && name != "claude" {
		return "", errors.New("未知 AI，请选择 Codex 或 Claude Code")
	}
	if p := os.Getenv("DAVINCI_" + strings.ToUpper(name) + "_BIN"); p != "" {
		if filepath.IsAbs(p) {
			return exec.LookPath(p)
		}
		return "", errors.New("AI 程序路径必须是绝对路径")
	}
	if p, err := exec.LookPath(name); err == nil && !strings.HasSuffix(strings.ToLower(p), ".cmd") {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	dirs := []string{filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".npm-global", "bin")}
	if runtime.GOOS == "windows" {
		name += ".exe"
		dirs = append(dirs, filepath.Join(home, "AppData", "Local", "Microsoft", "WinGet", "Links"))
	}
	for _, dir := range dirs {
		if p, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("未找到 %s，请先安装并在终端登录，再刷新检测；自定义安装可设置 DAVINCI_%s_BIN", name, strings.ToUpper(strings.TrimSuffix(name, ".exe")))
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > b.limit {
		return 0, errors.New("AI 输出超过大小限制")
	}
	return b.Buffer.Write(p)
}

func Run(ctx context.Context, req Request) (*Response, error) {
	bin, err := executable(req.Provider)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "davinci-comment-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	schemaPath, resultPath := filepath.Join(dir, "schema.json"), filepath.Join(dir, "result.json")
	if err := os.WriteFile(schemaPath, []byte(responseSchema), 0600); err != nil {
		return nil, err
	}
	var args []string
	input := req.Prompt
	if req.Provider == "codex" {
		args = []string{"exec", "--skip-git-repo-check", "--sandbox", "read-only", "--ephemeral", "--color", "never", "--output-schema", schemaPath, "--output-last-message", resultPath}
		if len(req.Image) > 0 {
			p := filepath.Join(dir, "board.png")
			if err := os.WriteFile(p, req.Image, 0600); err != nil {
				return nil, err
			}
			args = append(args, "--image", p)
		}
		args = append(args, "-")
	} else {
		args = []string{"-p", "--output-format", "stream-json", "--verbose", "--input-format", "stream-json", "--json-schema", responseSchema, "--tools", "", "--strict-mcp-config", "--no-session-persistence", "--permission-mode", "dontAsk", "--settings", `{"disableAllHooks":true}`}
		content := []any{map[string]any{"type": "text", "text": req.Prompt}}
		if len(req.Image) > 0 {
			content = append(content, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": base64.StdEncoding.EncodeToString(req.Image)}})
		}
		b, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}})
		input = string(b) + "\n"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	configureProcess(cmd)
	cmd.Dir = dir
	// Keep saved login/provider configuration; no prompt or image enters shell args.
	cmd.Env = os.Environ()
	if runtime.GOOS != "windows" {
		cmd.Env = append(cmd.Env, "PATH="+filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH")+":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin")
	}
	cmd.Stdin = strings.NewReader(input)
	stdout, stderr := &limitedBuffer{limit: 2 << 20}, &limitedBuffer{limit: 128 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 3 * time.Second
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		message := strings.ToLower(stdout.String() + stderr.String())
		if strings.Contains(message, "not logged in") || strings.Contains(message, "please run /login") || strings.Contains(message, "authentication required") {
			return nil, fmt.Errorf("%s 尚未登录，请先在终端打开该 AI 并完成登录，再重试评论", req.Provider)
		}
		// Do not persist CLI stderr: it can contain authentication data or full prompts.
		return nil, fmt.Errorf("%s 执行失败（%v）。请在终端检查登录状态、模型配置及版本", req.Provider, err)
	}
	var raw []byte
	if req.Provider == "codex" {
		f, err := os.Open(resultPath)
		if err != nil {
			return nil, errors.New("Codex 没有返回修改结果")
		}
		defer f.Close()
		raw, err = io.ReadAll(io.LimitReader(f, 1<<20))
		if err != nil {
			return nil, err
		}
	} else {
		var out struct {
			Type       string          `json:"type"`
			Structured json.RawMessage `json:"structured_output"`
			IsError    bool            `json:"is_error"`
		}
		for _, line := range bytes.Split(stdout.Bytes(), []byte("\n")) {
			var event struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(line, &event) == nil && event.Type == "result" {
				if err := json.Unmarshal(line, &out); err != nil {
					return nil, errors.New("Claude Code 返回的结果格式不正确")
				}
			}
		}
		if out.Type != "result" || out.IsError || len(out.Structured) == 0 {
			return nil, errors.New("Claude Code 没有返回有效修改结果，请检查登录状态与版本")
		}
		raw = out.Structured
	}
	var out Response
	if json.Unmarshal(raw, &out) != nil || strings.TrimSpace(out.Summary) == "" || !json.Valid([]byte(out.Commands)) {
		return nil, errors.New("AI 返回的修改格式不正确，请重试")
	}
	return &out, nil
}
