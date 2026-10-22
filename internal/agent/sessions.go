package agent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Sessions owns one Codex app-server and a separate durable thread per project.
// Warming only loads context into history; it never starts a model turn.
type Sessions struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	dir      string
	process  *codexProcess
	projects map[string]*projectSession
	// Threads are saved with a fingerprint of the conversation they belong to.
	load func(project, scope string) (string, error)
	save func(project, thread, scope string) error
}

type projectSession struct {
	gate        chan struct{}
	thread      string
	process     *codexProcess
	scope       [32]byte
	contextHash [32]byte
}

func NewSessions(dir string, load func(string, string) (string, error), save func(string, string, string) error) *Sessions {
	ctx, cancel := context.WithCancel(context.Background())
	return &Sessions{ctx: ctx, cancel: cancel, dir: dir, projects: map[string]*projectSession{}, load: load, save: save}
}

func (s *Sessions) Close() {
	s.cancel()
	s.mu.Lock()
	p := s.process
	s.mu.Unlock()
	if p != nil {
		p.stop()
	}
}

func (s *Sessions) project(ctx context.Context, id string) (*projectSession, error) {
	if id == "" {
		return nil, errors.New("缺少项目 ID")
	}
	s.mu.Lock()
	p := s.projects[id]
	if p == nil {
		p = &projectSession{gate: make(chan struct{}, 1)}
		s.projects[id] = p
	}
	s.mu.Unlock()
	select {
	case p.gate <- struct{}{}:
		return p, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, errors.New("AI 服务已关闭")
	}
}

func (s *Sessions) connection(ctx context.Context) (*codexProcess, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return nil, errors.New("AI 服务已关闭")
	}
	if s.process != nil && s.process.alive() {
		return s.process, nil
	}
	p, err := startCodexProcess(s.ctx, ctx, s.dir)
	if err != nil {
		return nil, err
	}
	s.process = p
	return p, nil
}

func (s *Sessions) ensure(ctx context.Context, p *projectSession, req Request, refresh bool) (*codexProcess, error) {
	proc, err := s.connection(ctx)
	if err != nil {
		return nil, err
	}
	// A thread belongs to one conversation under one set of rules: a new
	// conversation starts afresh, and a thread keeps the answers it gave under
	// its rules ("boards can't be added"). Only a thread started for both is resumed.
	scope := sha256.Sum256([]byte(req.Session + "\x00" + req.Instructions))
	if p.process != proc || p.scope != scope {
		if p.scope != scope {
			p.thread = ""
			if s.load != nil {
				p.thread, err = s.load(req.ProjectID, hex.EncodeToString(scope[:]))
				if err != nil {
					return nil, err
				}
			}
		}
		params := map[string]any{"cwd": s.dir, "approvalPolicy": "never", "sandbox": "read-only", "developerInstructions": req.Instructions, "config": proc.config}
		method := "thread/start"
		if p.thread != "" {
			method = "thread/resume"
			params["threadId"] = p.thread
			params["excludeTurns"] = true
		}
		var result struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		err = proc.call(ctx, method, params, &result)
		// A user may delete an old Codex thread. Starting a fresh context is safe:
		// no model turn has been submitted and no editing request is replayed.
		var rpcErr *codexRPCError
		if errors.As(err, &rpcErr) && method == "thread/resume" && (strings.Contains(strings.ToLower(rpcErr.Message), "not found") || strings.Contains(strings.ToLower(rpcErr.Message), "no rollout")) {
			delete(params, "threadId")
			delete(params, "excludeTurns")
			err = proc.call(ctx, "thread/start", params, &result)
		}
		if err != nil {
			return nil, err
		}
		if result.Thread.ID == "" {
			return nil, errors.New("Codex 未返回会话 ID")
		}
		if s.save != nil {
			if err = s.save(req.ProjectID, result.Thread.ID, hex.EncodeToString(scope[:])); err != nil {
				return nil, err
			}
		}
		p.thread = result.Thread.ID
		p.process = proc
		p.scope = scope
		p.contextHash = [32]byte{}
	}
	hash := sha256.Sum256([]byte(req.Context))
	if req.Context != "" && (refresh || p.contextHash == [32]byte{}) && p.contextHash != hash {
		items := []any{map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": req.Context}}}}
		if err = proc.call(ctx, "thread/inject_items", map[string]any{"threadId": p.thread, "items": items}, nil); err != nil {
			return nil, err
		}
		p.contextHash = hash
	}
	return proc, nil
}

func (s *Sessions) Warm(ctx context.Context, req Request) (string, error) {
	p, err := s.project(ctx, req.ProjectID)
	if err != nil {
		return "", err
	}
	defer func() { <-p.gate }()
	_, err = s.ensure(ctx, p, req, true)
	return p.thread, err
}

func (s *Sessions) Run(ctx context.Context, req Request) (*Response, error) {
	p, err := s.project(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	defer func() { <-p.gate }()
	proc, err := s.ensure(ctx, p, req, false)
	if err != nil {
		return nil, err
	}
	events := make(chan codexEvent, 128)
	proc.mu.Lock()
	proc.turns[p.thread] = events
	proc.mu.Unlock()
	defer func() { proc.mu.Lock(); delete(proc.turns, p.thread); proc.mu.Unlock() }()
	input := []any{map[string]any{"type": "text", "text": req.Prompt}}
	if len(req.Image) > 0 {
		input = append(input, map[string]any{"type": "image", "url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(req.Image)})
	}
	var schema any
	_ = json.Unmarshal([]byte(responseSchema), &schema)
	var started struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err = proc.call(ctx, "turn/start", map[string]any{"threadId": p.thread, "input": input, "outputSchema": schema}, &started); err != nil {
		// A lost start response has an unknown outcome. Never replay it automatically.
		proc.stop()
		return nil, err
	}
	var final string
	for {
		select {
		case event := <-events:
			if event.TurnID != "" && event.TurnID != started.Turn.ID {
				continue
			}
			if event.Method == "item/completed" && event.Item.Type == "agentMessage" {
				final = event.Item.Text
			}
			if event.Method == "turn/completed" && event.Turn.ID == started.Turn.ID {
				if event.Turn.Status != "completed" {
					return nil, errors.New("Codex 修改未完成，请检查登录状态后重试")
				}
				for _, item := range event.Turn.Items {
					if item.Type == "agentMessage" {
						final = item.Text
					}
				}
				var out Response
				if len(final) > 1<<20 || json.Unmarshal([]byte(final), &out) != nil || strings.TrimSpace(out.Summary) == "" || !json.Valid([]byte(out.Commands)) {
					return nil, errors.New("Codex 返回的修改格式不正确")
				}
				return &out, nil
			}
		case <-ctx.Done():
			stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = proc.call(stopCtx, "turn/interrupt", map[string]any{"threadId": p.thread, "turnId": started.Turn.ID}, nil)
			// Do not release this project's queue until the old turn really stopped.
			stopped := false
			for !stopped {
				select {
				case event := <-events:
					stopped = event.Method == "turn/completed" && event.Turn.ID == started.Turn.ID
				case <-proc.done:
					stopped = true
				case <-stopCtx.Done():
					proc.stop()
					stopped = true
				}
			}
			cancel()
			return nil, ctx.Err()
		case <-proc.done:
			return nil, errors.New("Codex 连接已中断，请重新提交")
		}
	}
}

type codexRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *codexRPCError) Error() string {
	return fmt.Sprintf("Codex 会话请求失败（%d），请检查版本和登录状态", e.Code)
}

type codexEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *codexRPCError  `json:"error"`
}
type codexItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type codexEvent struct {
	Method   string
	ThreadID string    `json:"threadId"`
	TurnID   string    `json:"turnId"`
	Item     codexItem `json:"item"`
	Turn     struct {
		ID     string      `json:"id"`
		Status string      `json:"status"`
		Items  []codexItem `json:"items"`
	} `json:"turn"`
}
type codexProcess struct {
	mu      sync.Mutex
	writeMu sync.Mutex
	seq     int
	pending map[string]chan codexEnvelope
	turns   map[string]chan codexEvent
	input   io.WriteCloser
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	done    chan struct{}
	config  map[string]any
}

func startCodexProcess(lifetime, ctx context.Context, dir string) (*codexProcess, error) {
	bin, err := executable("codex")
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	root, cancel := context.WithCancel(lifetime)
	cmd := exec.CommandContext(root, bin, "app-server", "--listen", "stdio://")
	configureProcess(cmd)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	if runtime.GOOS != "windows" {
		cmd.Env = append(cmd.Env, "PATH="+filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH")+":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin")
	}
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 3 * time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	p := &codexProcess{cmd: cmd, input: in, cancel: cancel, done: make(chan struct{}), pending: map[string]chan codexEnvelope{}, turns: map[string]chan codexEvent{}}
	if err = cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	go p.read(out)
	go func() { _ = cmd.Wait(); close(p.done) }()
	if err = p.call(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "davinci", "title": "DaVinci", "version": "1.0.0"}, "capabilities": map[string]any{"experimentalApi": true}}, nil); err == nil {
		err = p.write(map[string]any{"method": "initialized"})
	}
	if err != nil {
		p.stop()
		return nil, err
	}
	// Disable inherited integrations only in our threads, preserving login/model
	// configuration and leaving the user's global Codex configuration untouched.
	var cfg struct {
		Config struct {
			MCP map[string]any `json:"mcp_servers"`
		} `json:"config"`
	}
	if err = p.call(ctx, "config/read", map[string]any{"includeLayers": false}, &cfg); err != nil {
		p.stop()
		return nil, err
	}
	p.config = map[string]any{"project_doc_max_bytes": 0, "web_search": "disabled"}
	for name := range cfg.Config.MCP {
		p.config["mcp_servers."+name+".enabled"] = false
	}
	for _, feature := range []string{"shell_tool", "unified_exec", "apply_patch_freeform", "plugins", "apps", "codex_hooks", "hooks", "memories", "memory_tool", "multi_agent", "computer_use", "browser_use", "image_generation"} {
		p.config["features."+feature] = false
	}
	p.config["features.skip_host_skill_discovery"] = true
	return p, nil
}

func (p *codexProcess) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}
func (p *codexProcess) stop() { p.cancel(); _ = p.input.Close(); <-p.done }
func (p *codexProcess) write(v any) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return json.NewEncoder(p.input).Encode(v)
}
func (p *codexProcess) call(ctx context.Context, method string, params any, out any) error {
	p.mu.Lock()
	p.seq++
	id := fmt.Sprint(p.seq)
	reply := make(chan codexEnvelope, 1)
	p.pending[id] = reply
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.pending, id); p.mu.Unlock() }()
	if err := p.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return errors.New("Codex 连接已中断")
	}
	select {
	case r := <-reply:
		if r.Error != nil {
			return r.Error
		}
		if out != nil {
			return json.Unmarshal(r.Result, out)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return errors.New("Codex 进程已退出，请检查版本和登录状态")
	}
}
func (p *codexProcess) read(out io.Reader) {
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	for scanner.Scan() {
		var msg codexEnvelope
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		if len(msg.ID) > 0 {
			if msg.Method != "" {
				// This adapter only accepts structured edits; no interactive tool requests.
				_ = p.write(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32601, "message": "Tools and approvals are unavailable in DaVinci comment editing"}})
				continue
			}
			var id string
			_ = json.Unmarshal(msg.ID, &id)
			p.mu.Lock()
			ch := p.pending[id]
			p.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
			continue
		}
		if msg.Method != "item/completed" && msg.Method != "turn/completed" {
			continue
		}
		var event codexEvent
		if json.Unmarshal(msg.Params, &event) != nil {
			continue
		}
		event.Method = msg.Method
		if event.Method == "item/completed" && event.Item.Type != "agentMessage" {
			continue
		}
		p.mu.Lock()
		ch := p.turns[event.ThreadID]
		p.mu.Unlock()
		if ch != nil {
			select {
			case ch <- event:
			default:
				p.cancel()
			}
		}
	}
	p.cancel()
}
