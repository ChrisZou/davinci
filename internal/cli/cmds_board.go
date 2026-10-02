package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"davinci/internal/server"
)

// Boards: a project holds one or more 画板. `davinci boards` lists them; the
// `board` subcommands add, copy, remove, rename, reorder and switch. Every
// other command takes -b to act on a board other than the current one.

func newBoardsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "boards",
		Short: "列出项目的全部画板",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			boards, err := c.Boards()
			if err != nil {
				return err
			}
			if c.JSON {
				printJSON(map[string]any{"boards": boards})
				return nil
			}
			for _, b := range boards {
				mark := " "
				if b.Active {
					mark = "*"
				}
				fmt.Printf("%s %d  %-14s %5d×%-5d %3d 个图层  %s\n", mark, b.Index, b.Name, b.Width, b.Height, b.Layers, b.ID)
			}
			return nil
		},
	}
}

func newBoardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "board",
		Short: "新建 / 复制 / 删除 / 重命名 / 排序 / 切换画板",
		Example: `  davinci board add --name 原图 --w 1242 --h 1656
  davinci board dup 1
  davinci board rename 2 原图
  davinci board mv 原图 1
  davinci board use 2          # 之后不带 -b 的命令都作用于它
  davinci board rm 3`,
	}
	var (
		name   string
		preset string
		w, h   int
		bg     string
		at     int
	)
	add := &cobra.Command{
		Use:   "add",
		Short: "新建画板（默认同当前画板尺寸，放在它后面）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			m := map[string]any{"type": "addBoard"}
			if name != "" {
				m["name"] = name
			}
			if preset != "" {
				m["preset"] = preset
			}
			if w > 0 {
				m["width"] = w
			}
			if h > 0 {
				m["height"] = h
			}
			if bg != "" {
				m["background"] = bg
			}
			if at > 0 {
				m["index"] = at
			}
			return runBoard(m)
		},
	}
	add.Flags().StringVar(&name, "name", "", "画板名")
	add.Flags().StringVar(&preset, "preset", "", "画布预设（xhs-3-4、bili-16-9 …）")
	add.Flags().IntVar(&w, "w", 0, "宽")
	add.Flags().IntVar(&h, "h", 0, "高")
	add.Flags().StringVar(&bg, "bg", "", "背景色")
	add.Flags().IntVar(&at, "at", 0, "放在第几个（从 1 开始）")

	var dupName string
	dup := &cobra.Command{
		Use:   "dup [画板]",
		Short: "复制画板（连同全部图层）",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m := map[string]any{"type": "duplicateBoard"}
			if len(args) == 1 {
				m["id"] = args[0]
			}
			if dupName != "" {
				m["name"] = dupName
			}
			return runBoard(m)
		},
	}
	dup.Flags().StringVar(&dupName, "name", "", "副本名")

	cmd.AddCommand(
		add,
		dup,
		&cobra.Command{
			Use:   "rm <画板>",
			Short: "删除画板",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return runBoard(map[string]any{"type": "removeBoard", "id": args[0]})
			},
		},
		&cobra.Command{
			Use:   "rename <画板> <新名称>",
			Short: "重命名画板",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				return runBoard(map[string]any{"type": "renameBoard", "id": args[0], "name": args[1]})
			},
		},
		&cobra.Command{
			Use:   "mv <画板> <位置>",
			Short: "把画板移到第几个（从 1 开始；第一个是项目的封面缩略图）",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				var n int
				if _, err := fmt.Sscanf(args[1], "%d", &n); err != nil || n < 1 {
					return fmt.Errorf("位置要是从 1 开始的数字：%s", args[1])
				}
				return runBoard(map[string]any{"type": "moveBoard", "id": args[0], "index": n})
			},
		},
		&cobra.Command{
			Use:     "use <画板>",
			Aliases: []string{"select", "switch"},
			Short:   "切换当前画板",
			Args:    cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return runBoard(map[string]any{"type": "selectBoard", "id": args[0]})
			},
		},
	)
	return cmd
}

// runBoard runs one board command and prints the board it landed on. -b makes
// no sense here (the board is the argument), so it is not forwarded.
func runBoard(m map[string]any) error {
	c, err := NewClient()
	if err != nil {
		return err
	}
	saved := gf.board
	gf.board = ""
	res, err := c.Exec(m)
	gf.board = saved
	if err != nil {
		return err
	}
	if c.JSON {
		printJSON(res)
		return nil
	}
	boards, err := c.Boards()
	if err != nil {
		return err
	}
	var parts []string
	for _, b := range boards {
		label := fmt.Sprintf("%d:%s", b.Index, b.Name)
		if b.Active {
			label = "[" + label + "]"
		}
		parts = append(parts, label)
	}
	fmt.Println(strings.Join(parts, "  "))
	return nil
}

// Boards lists the project's boards.
func (c *Client) Boards() ([]server.BoardInfo, error) {
	id, err := c.ProjectID()
	if err != nil {
		return nil, err
	}
	var out struct {
		OK     bool               `json:"ok"`
		Boards []server.BoardInfo `json:"boards"`
		Error  string             `json:"error"`
	}
	if err := c.getJSON("/api/projects/"+id+"/boards", &out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("%s", firstNonEmpty(out.Error, "list boards failed"))
	}
	return out.Boards, nil
}
