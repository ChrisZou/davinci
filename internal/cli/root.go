// Package cli is the cobra layer in front of davinci's HTTP API. Every command
// is thin: it builds a command object, hands it to the server, which applies
// it, and prints what came back.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// globalFlagSet holds the flags every subcommand shares.
type globalFlags struct {
	server  string
	project string
	data    string
	port    int
	json    bool
	quiet   bool
	board   string
}

var gf globalFlags

// newRootCmd assembles the whole command tree.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "davinci",
		Short: "AI Native 的本地图片编辑器",
		Long: `davinci — 一个图层化、AI 可编程的本地图片编辑器。

人用编辑器界面，AI 用这里的命令（或 HTTP API），两者改的是同一份文档、
同一个撤销栈。缺省操作最近更新的项目。`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&gf.server, "server", "", "davinci 服务地址（默认 http://127.0.0.1:7789）")
	root.PersistentFlags().StringVarP(&gf.project, "project", "p", "", "项目 id 或名称（缺省取最近更新的）")
	root.PersistentFlags().StringVar(&gf.data, "data", "", "数据目录（默认：davinci 仓库下的 data/，也可用 DAVINCI_DATA）")
	root.PersistentFlags().IntVar(&gf.port, "port", 0, "服务端口（仅 serve 使用；其余命令用 --server）")
	root.PersistentFlags().BoolVar(&gf.json, "json", false, "以 JSON 输出")
	root.PersistentFlags().BoolVar(&gf.quiet, "quiet", false, "少打日志")
	root.PersistentFlags().StringVarP(&gf.board, "board", "b", "", "画板 id、名称或序号（从 1 开始；缺省为当前画板）")

	root.AddCommand(
		newServeCmd(),
		newProjectsCmd(),
		newNewCmd(),
		newOpenCmd(),
		newLayerCmd(),
		newLayersCmd(),
		newTextCmd(),
		newImageCmd(),
		newCanvasCmd(),
		newFontCmd(),
		newFontsCmd(),
		newDocCmd(),
		newRenderCmd(),
		newWatchCmd(),
		newSchemaCmd(),
		newExecCmd(),
		newUndoCmd(),
		newRedoCmd(),
		newLibraryCmd(),
		newTemplateCmd(),
		newBoardsCmd(),
		newBoardCmd(),
	)
	return root
}

// Execute runs the CLI and returns a process exit code.
func Execute() int {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "davinci: %v\n", err)
		return 1
	}
	return 0
}
