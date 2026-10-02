package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/spf13/cobra"
)

// newWatchCmd streams document changes so an agent can observe a human's manual
// edits. It prints one line per change and never exits on its own.
func newWatchCmd() *cobra.Command {
	var interval time.Duration
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "流式打印文档变更（AI 可由此观察人的手动修改）",
		Long: `订阅当前项目的文档变更，把每次变化打印成一行 JSON：
  {"revision":12,"layers":5,"note":"AI 命令执行后"}

Ctrl-C 退出。想要原始文档就加 --raw。`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			id, err := c.ProjectID()
			if err != nil {
				return err
			}
			_ = interval
			return watch(c, id)
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "轮询间隔（WebSocket 不可用时退化为轮询）")
	return cmd
}

// watch prefers the WebSocket and falls back to polling /api/projects/:id.
func watch(c *Client, id string) error {
	wsURL := "ws" + strings.TrimPrefix(c.BaseURL, "http") + "/ws?project=" + id
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return watchByPolling(c, id, 2*time.Second)
	}
	defer conn.Close()

	var lastRev int64 = -1
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	// A watcher only listens: it is told of every change, whoever made it.
	report := func(rev int64, note string, command json.RawMessage) {
		if rev == lastRev {
			return
		}
		lastRev = rev
		layers, err := c.Layers()
		n := 0
		if err == nil {
			n = len(layers)
		}
		row := map[string]any{
			"revision": rev, "layers": n, "project": id, "note": note,
			"at": time.Now().Format(time.RFC3339),
		}
		if len(command) > 0 {
			row["command"] = command
		}
		b, _ := json.Marshal(row)
		fmt.Fprintln(out, string(b))
		out.Flush()
	}

	for {
		var env struct {
			Type     string          `json:"type"`
			Document json.RawMessage `json:"document"`
			Revision int64           `json:"revision"`
			Command  json.RawMessage `json:"command"`
		}
		if err := conn.ReadJSON(&env); err != nil {
			return nil // connection closed
		}
		switch env.Type {
		case "hello":
			report(env.Revision, "connected", nil)
		case "doc":
			report(env.Revision, "document changed", env.Command)
		}
	}
}

// watchByPolling is the fallback for environments where WebSocket cannot be
// dialled (some corporate proxies, or a headless environment).
func watchByPolling(c *Client, id string, every time.Duration) error {
	var last int64 = -1
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for {
		var p struct {
			OK      bool          `json:"ok"`
			Project serverProject `json:"project"`
		}
		if err := c.getJSON("/api/projects/"+id, &p); err == nil && p.OK {
			if p.Project.Revision != last {
				last = p.Project.Revision
				b, _ := json.Marshal(map[string]any{
					"revision": last, "project": id, "note": "polled", "at": time.Now().Format(time.RFC3339),
				})
				fmt.Fprintln(out, string(b))
				out.Flush()
			}
		}
		time.Sleep(every)
	}
}

// serverProject is the polling view of a project (revision only matters here).
type serverProject struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Revision int64  `json:"revision"`
}
