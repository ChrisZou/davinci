package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"davinci/internal/server"
)

// Client talks to a running davinci server over HTTP.
type Client struct {
	BaseURL string
	Project string
	HTTP    *http.Client
	JSON    bool

	webURL string // cached by WebURL
}

// env helpers -------------------------------------------------------------

const defaultPort = server.DefaultPort

func dataDir() string {
	if gf.data != "" {
		return gf.data
	}
	return server.DefaultDataDir()
}

func serverURL() string {
	if gf.server != "" {
		return strings.TrimRight(gf.server, "/")
	}
	if v := os.Getenv("DAVINCI_SERVER"); v != "" {
		return strings.TrimRight(v, "/")
	}
	port := defaultPort
	if v := os.Getenv("DAVINCI_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			port = n
		}
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func portOf(url string) int {
	i := strings.LastIndex(url, ":")
	if i < 0 {
		return defaultPort
	}
	n, err := strconv.Atoi(url[i+1:])
	if err != nil {
		return defaultPort
	}
	return n
}

// NewClient returns a client and makes sure a server is reachable, starting one
// in the background when it is not. An AI agent should never have to manage the
// davinci process itself. Project resolution is deliberately lazy: `new` and
// `projects` must work on an empty database.
func NewClient() (*Client, error) {
	c := &Client{
		BaseURL: serverURL(),
		// --project / DAVINCI_PROJECT, or empty to mean "the project I touched
		// last". Leaving it empty here is what makes `resolveProject` kick in.
		Project: firstNonEmpty(gf.project, os.Getenv("DAVINCI_PROJECT")),
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
		JSON:    gf.json,
	}
	if err := c.EnsureServer(); err != nil {
		return nil, err
	}
	return c, nil
}

// EnsureServer starts `davinci serve` in the background if nothing answers.
func (c *Client) EnsureServer() error {
	if c.ping() {
		return nil
	}
	logPath := filepath.Join(dataDir(), "serve.log")
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		logFile = nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"serve", "--port", strconv.Itoa(portOf(c.BaseURL)), "--quiet", "--data", dataDir()}
	cmd := exec.Command(exe, args...)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if logFile != nil {
		cmd.Stdout, cmd.Stderr = logFile, logFile
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start davinci serve: %w", err)
	}
	_ = cmd.Process.Release()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if c.ping() {
			return nil
		}
		time.Sleep(120 * time.Millisecond)
	}
	return fmt.Errorf("davinci 服务启动超时，日志在 %s", logPath)
}

func (c *Client) ping() bool {
	resp, err := c.HTTP.Get(c.BaseURL + "/api/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// resolveProject fills in the default project — the most recently updated work
// (templates are not picked by default) — when --project was not given. It is a no-op once a project is known.
func (c *Client) resolveProject() error {
	if c.Project != "" {
		return nil
	}
	var out struct {
		OK       bool                    `json:"ok"`
		Projects []server.ProjectSummary `json:"projects"`
		Error    string                  `json:"error"`
	}
	if err := c.getJSON("/api/projects", &out); err != nil {
		return err
	}
	if !out.OK {
		return fmt.Errorf("%s", firstNonEmpty(out.Error, "list projects failed"))
	}
	if len(out.Projects) == 0 {
		return fmt.Errorf("还没有项目，先用 `davinci new \"名字\"` 创建一个")
	}
	c.Project = out.Projects[0].ID
	return nil
}

// ProjectID resolves --project (id or exact name) to an id, falling back to the
// most recently updated project.
func (c *Client) ProjectID() (string, error) {
	if err := c.resolveProject(); err != nil {
		return "", err
	}
	// Only an id-shaped ref is worth probing with: `/api/projects/<name>` answers
	// 404, and letting that abort the call would make `-p 名称` impossible.
	if strings.HasPrefix(c.Project, "p_") {
		var out struct {
			OK      bool           `json:"ok"`
			Project server.Project `json:"project"`
		}
		if err := c.getJSON("/api/projects/"+c.Project, &out); err == nil && out.OK && out.Project.ID != "" {
			return out.Project.ID, nil
		}
	}
	// Not an id (or no such id): try every project name.
	var list struct {
		OK       bool                    `json:"ok"`
		Projects []server.ProjectSummary `json:"projects"`
	}
	// Templates are projects too, and can be named with -p like any work.
	if err := c.getJSON("/api/projects?kind=all", &list); err != nil {
		return "", err
	}
	for _, p := range list.Projects {
		if p.Name == c.Project {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("找不到项目：%s（用 `davinci projects` 看有哪些）", c.Project)
}

// --- HTTP plumbing ---

func (c *Client) getJSON(path string, out any) error {
	resp, err := c.HTTP.Get(c.BaseURL + path)
	if err != nil {
		return fmt.Errorf("请求 %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode/100 != 2 {
		return apiError(body, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func (c *Client) postJSON(path string, payload, out any) error {
	var body []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = b
	}
	resp, err := c.HTTP.Post(c.BaseURL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("请求 %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode/100 != 2 {
		return apiError(raw, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// apiError pulls the "error" field out of a failed response body.
func apiError(body []byte, status int) error {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return fmt.Errorf("%s", e.Error)
	}
	if len(body) > 200 {
		body = body[:200]
	}
	return fmt.Errorf("HTTP %d: %s", status, strings.TrimSpace(string(body)))
}

// delete issues a DELETE request, surfacing the server's error message.
func (c *Client) delete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("请求 %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return apiError(raw, resp.StatusCode)
	}
	return nil
}

// getRaw fetches a path and returns the body verbatim (for `davinci schema`).
func (c *Client) getRaw(path string) ([]byte, error) {
	resp, err := c.HTTP.Get(c.BaseURL + path)
	if err != nil {
		return nil, fmt.Errorf("请求 %s: %w", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		return nil, apiError(b, resp.StatusCode)
	}
	return b, nil
}

// putDoc replaces a project's whole document.
func (c *Client) putDoc(id string, doc []byte) error {
	req, err := http.NewRequest(http.MethodPut, c.BaseURL+"/api/projects/"+id, bytes.NewReader(doc))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("导入文档: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return apiError(raw, resp.StatusCode)
	}
	return nil
}

// uploadFont posts a .ttf/.otf file and returns its served URL.
func (c *Client) uploadFont(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("读取 %s: %w", path, err)
	}
	defer f.Close()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	w, err := mw.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(w, f); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	resp, err := c.HTTP.Post(c.BaseURL+"/api/fonts", mw.FormDataContentType(), &buf)
	if err != nil {
		return "", fmt.Errorf("上传字体: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return "", apiError(raw, resp.StatusCode)
	}
	var out struct {
		URL    string `json:"url"`
		Family string `json:"family"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", fmt.Errorf("上传失败: %s", firstNonEmpty(out.Error, "no url"))
	}
	return out.URL, nil
}

// execAndPrint runs commands and prints the resulting layer summary, so every
// subcommand produces the same output.
func (c *Client) execAndPrint(cmds ...map[string]any) error {
	res, err := c.Exec(cmds...)
	if err != nil {
		return err
	}
	if c.JSON {
		printJSON(res)
		return nil
	}
	if res.Data == nil {
		return nil
	}
	// A batch answers with one datum per command, so the caller sees the last
	// one — the same line a single command would have printed.
	data := res.Data
	if trimmed := trimSpace(string(data)); strings.HasPrefix(trimmed, "[") {
		var results []json.RawMessage
		if err := json.Unmarshal(data, &results); err != nil || len(results) == 0 {
			fmt.Println(compactJSON(data))
			return nil
		}
		data = results[len(results)-1]
	}
	var out struct {
		ID      string         `json:"id"`
		Type    string         `json:"type"`
		Name    string         `json:"name"`
		Layer   map[string]any `json:"layer"`
		Layers  []map[string]any
		Result  string `json:"message"`
		Visible *bool  `json:"visible"`
		Locked  *bool  `json:"locked"`
	}
	if json.Unmarshal(data, &out) == nil {
		switch {
		case out.Layer != nil:
			fmt.Printf("%s (%v)\n", out.Layer["id"], out.Layer["type"])
		case len(out.Layers) > 0:
			fmt.Printf("ok，当前 %d 个图层\n", len(out.Layers))
		// A full Layer also carries visible/locked, so only read them as the
		// answer to hide/show and lock/unlock when there is no layer to show.
		// Otherwise every image-style command reports "已显示".
		case out.Visible != nil && out.Type == "":
			// `hide` is a command, not a flag round trip, so the answer should
			// read like one.
			fmt.Printf("%s %s\n", layerLabel(out.ID, out.Name), onOff(*out.Visible, "已显示", "已隐藏"))
		case out.Locked != nil && out.Type == "":
			fmt.Printf("%s %s\n", layerLabel(out.ID, out.Name), onOff(*out.Locked, "已锁定", "已解锁"))
		case out.Result != "":
			fmt.Println(out.Result)
		case out.ID != "":
			// Most commands answer with the layer they touched. Saying which
			// one beats printing nothing: the caller — often an AI agent —
			// needs to know the edit landed and on what.
			what := firstNonEmpty(out.Type, out.Name)
			if what != "" {
				fmt.Printf("%s (%s)\n", out.ID, what)
			} else {
				fmt.Println(out.ID)
			}
		default:
			// Anything else (a bare {rotation:12}, say) still gets echoed,
			// compactly, rather than swallowed.
			fmt.Println(compactJSON(data))
		}
	}
	return nil
}

// layerLabel names the layer a command touched, preferring its name: "标题" over
// "t_9ohh1m45r" is what a person reading the output actually wants.
func layerLabel(id, name string) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	return strings.TrimSpace(id)
}

func onOff(v bool, on, off string) string {
	if v {
		return on
	}
	return off
}

func trimSpace(s string) string { return strings.TrimSpace(s) }

// compactJSON renders a command's data payload on one line, for the cases where
// there is nothing summarisable to print.
func compactJSON(b json.RawMessage) string {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return strings.TrimSpace(string(b))
	}
	out, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(string(b))
	}
	return string(out)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// listProjects returns the projects of a kind ("design", "template" or
// "all"), newest first.
func (c *Client) listProjects(kind string) ([]server.ProjectSummary, error) {
	var out struct {
		OK       bool                    `json:"ok"`
		Projects []server.ProjectSummary `json:"projects"`
		Error    string                  `json:"error"`
	}
	if err := c.getJSON("/api/projects?kind="+url.QueryEscape(kind), &out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("%s", firstNonEmpty(out.Error, "list projects failed"))
	}
	return out.Projects, nil
}

// --- editor operations ---

// Exec sends commands to the server and returns the result.
func (c *Client) Exec(cmds ...map[string]any) (*server.Result, error) {
	id, err := c.ProjectID()
	if err != nil {
		return nil, err
	}
	// -b names the board every command runs on; a command that names its own
	// board keeps it.
	if b := strings.TrimSpace(gf.board); b != "" {
		for _, cmd := range cmds {
			if _, ok := cmd["board"]; !ok {
				cmd["board"] = b
			}
		}
	}
	var payload any
	if len(cmds) == 1 {
		payload = cmds[0]
	} else {
		payload = map[string]any{"commands": cmds}
	}
	var out server.Result
	if err := c.postJSON("/api/projects/"+id+"/commands", payload, &out); err != nil {
		return nil, err
	}
	if !out.OK {
		msg := firstNonEmpty(out.Error, "command failed")
		return &out, fmt.Errorf("%s", msg)
	}
	return &out, nil
}

// Layers lists the project's layers, backmost first.
func (c *Client) Layers() ([]server.LayerRow, error) {
	id, err := c.ProjectID()
	if err != nil {
		return nil, err
	}
	var out struct {
		OK     bool              `json:"ok"`
		Layers []server.LayerRow `json:"layers"`
		Error  string            `json:"error"`
	}
	path := "/api/projects/" + id + "/layers"
	if b := strings.TrimSpace(gf.board); b != "" {
		path += "?board=" + url.QueryEscape(b)
	}
	if err := c.getJSON(path, &out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("%s", firstNonEmpty(out.Error, "list layers failed"))
	}
	return out.Layers, nil
}

// ResolveLayer turns a user-supplied "id or name" into a layer id.
func (c *Client) ResolveLayer(ref string) (*server.LayerRow, error) {
	layers, err := c.Layers()
	if err != nil {
		return nil, err
	}
	for i := range layers {
		if layers[i].ID == ref {
			return &layers[i], nil
		}
	}
	var matches []server.LayerRow
	for i := range layers {
		if layers[i].Name == ref {
			matches = append(matches, layers[i])
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("找不到图层：%s（用 `davinci layers` 看有哪些）", ref)
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("图层名 %s 有 %d 个重名，请改用 id", ref, len(matches))
	}
}

// UploadAsset imports a local file or remote URL and returns its /assets/ URL.
func (c *Client) UploadAsset(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("空的文件引用")
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		_ = mw.WriteField("url", ref)
	} else {
		path := strings.TrimPrefix(ref, "file://")
		f, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("读取 %s: %w", path, err)
		}
		defer f.Close()
		w, err := mw.CreateFormFile("file", filepath.Base(path))
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(w, f); err != nil {
			return "", err
		}
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	resp, err := c.HTTP.Post(c.BaseURL+"/api/assets", mw.FormDataContentType(), &buf)
	if err != nil {
		return "", fmt.Errorf("上传 %s: %w", ref, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode/100 != 2 {
		return "", apiError(raw, resp.StatusCode)
	}
	var out struct {
		Asset struct {
			URL string `json:"url"`
		} `json:"asset"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Asset.URL == "" {
		return "", fmt.Errorf("上传失败: %s", firstNonEmpty(out.Error, "no url"))
	}
	return out.Asset.URL, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
