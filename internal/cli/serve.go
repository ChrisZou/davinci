package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"davinci/internal/server"

	"github.com/spf13/cobra"
)

// newServeCmd runs the davinci server in the foreground.
func newServeCmd() *cobra.Command {
	var (
		port       int
		data       string
		open       bool
		noHeadless bool
		remote     bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "启动 davinci 服务（默认 127.0.0.1:7789）",
		RunE: func(cmd *cobra.Command, args []string) error {
			// The flag shadows the root's persistent --data, and neither one picks
			// up DAVINCI_DATA — but the store's own error message tells people to
			// set it, so honour it rather than let that hint point nowhere.
			dir := data
			if dir == "" {
				dir = gf.data
			}
			if dir == "" {
				dir = os.Getenv("DAVINCI_DATA")
			}
			opt := server.Options{Port: port, DataDir: dir, Quiet: gf.quiet, Remote: remote}
			srv, err := server.NewServer(opt)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			// Shutdown stops the renderer sidecar. The process must not exit
			// before it has finished, or that Node process outlives the server
			// as an orphan.
			shutdownDone := make(chan struct{})
			go func() {
				defer close(shutdownDone)
				<-ctx.Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx)
			}()
			if err := srv.Start(); err != nil {
				return err
			}
			if open {
				// After Start, so WebURL can see this server behind davinci.localhost.
				openBrowser((&Client{BaseURL: srv.Addr()}).WebURL())
			}
			select {
			case <-ctx.Done():
				<-shutdownDone
			case <-srv.Done():
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", server.DefaultPort, "监听端口")
	cmd.Flags().StringVar(&data, "data", "", "数据目录（默认：davinci 仓库下的 data/，或 DAVINCI_DATA）")
	cmd.Flags().BoolVar(&open, "open", false, "启动后打开浏览器")
	// Kept so existing service files keep working; there is no executor to
	// disable any more (the server applies commands itself).
	cmd.Flags().BoolVar(&noHeadless, "no-headless", false, "（已无作用，保留兼容）")
	_ = cmd.Flags().MarkHidden("no-headless")
	cmd.Flags().BoolVar(&remote, "remote", false, "部署到服务器时开启：素材导入不能读取本机路径、不能抓取内网地址")
	return cmd
}

// openBrowser opens the local editor in the user's browser. Failures are silent
// because the server prints the URL anyway.
func openBrowser(url string) {
	for _, bin := range []string{"open", "xdg-open"} {
		if _, err := execLookPath(bin); err == nil {
			_ = execRun(bin, url)
			return
		}
	}
}

// openEditor prints a project's editor URL and tries to open it.
func openEditor(c *Client, id string) error {
	url := c.EditorURL(id)
	c.print(url)
	openBrowser(url)
	return nil
}
