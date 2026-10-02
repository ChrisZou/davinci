package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"davinci/internal/server"
)

// newProjectsCmd lists projects, and carries the destructive subcommand.
func newProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "projects",
		Aliases: []string{"ls", "ps"},
		Short:   "列出所有项目",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			list, err := c.listProjects()
			if err != nil {
				return err
			}
			c.printProjects(list)
			return nil
		},
	}
	cmd.AddCommand(newRmProjectCmd())
	return cmd
}

// newNewCmd creates a project from a canvas preset or explicit size.
func newNewCmd() *cobra.Command {
	var (
		preset string
		width  int
		height int
		open   bool
	)
	cmd := &cobra.Command{
		Use:     "new [名称]",
		Aliases: []string{"create"},
		Short:   "新建项目（可用 --preset 选画布尺寸）",
		Example: `  davinci new "AI 编程封面" --preset xhs-3-4
  davinci new "方形图" --preset xhs-1-1`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			name := "未命名设计"
			if len(args) > 0 {
				name = args[0]
			}
			var out struct {
				OK      bool           `json:"ok"`
				Project server.Project `json:"project"`
				Editor  string         `json:"editorURL"`
				Error   string         `json:"error"`
			}
			body := map[string]any{"name": name, "preset": preset, "width": width, "height": height}
			if err := c.postJSON("/api/projects", body, &out); err != nil {
				return err
			}
			if !out.OK {
				return fmt.Errorf("%s", firstNonEmpty(out.Error, "create project failed"))
			}
			out.Editor = c.EditorURL(out.Project.ID)
			if c.JSON {
				printJSON(out)
				return nil
			}
			fmt.Printf("已创建 %s（%s，%d×%d）\n", out.Project.Name, out.Project.ID, out.Project.Width, out.Project.Height)
			fmt.Println(out.Editor)
			if open {
				return openEditor(c, out.Project.ID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&preset, "preset", "", fmt.Sprintf("画布预设：%v", presetKeys()))
	cmd.Flags().IntVar(&width, "w", 0, "画布宽度（不用预设时）")
	cmd.Flags().IntVar(&height, "h", 0, "画布高度（不用预设时）")
	cmd.Flags().BoolVar(&open, "open", false, "创建后在浏览器打开")
	return cmd
}

// newOpenCmd prints (and opens) a project's editor URL.
func newOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "open [项目 id|名称]",
		Aliases: []string{"edit"},
		Short:   "打开项目的编辑器页面",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			if len(args) > 0 {
				c.Project = args[0]
			}
			id, err := c.ProjectID()
			if err != nil {
				return err
			}
			return openEditor(c, id)
		},
	}
}

// newRmProjectCmd deletes projects. It is wired under `davinci projects rm`.
func newRmProjectCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rm [项目 id|名称...]",
		Aliases: []string{"delete"},
		Short:   "删除项目",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := NewClient()
			if err != nil {
				return err
			}
			for _, ref := range args {
				c.Project = ref
				id, err := c.ProjectID()
				if err != nil {
					return err
				}
				if err := c.delete("/api/projects/" + id); err != nil {
					return fmt.Errorf("删除 %s: %w", id, err)
				}
				fmt.Printf("已删除 %s\n", id)
			}
			return nil
		},
	}
}

func presetKeys() []string {
	out := []string{}
	for _, p := range server.Presets() {
		out = append(out, fmt.Sprintf("%s(%dx%d)", p.Key, p.Width, p.Height))
	}
	return out
}
