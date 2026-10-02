package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"davinci/internal/server"
)

// --- output helpers ---

// printJSON writes v as indented JSON.
func printJSON(v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	fmt.Println(string(b))
}

func (c *Client) print(v any) {
	if c.JSON {
		printJSON(v)
		return
	}
	fmt.Println(v)
}

// printLayer renders one layer as aligned label/value rows. `layer show` is
// usually an AI agent checking what it just wrote, so every field the row
// carries should be legible without parsing a Go struct.
func (c *Client) printLayer(l server.LayerRow) {
	if c.JSON {
		printJSON(l)
		return
	}
	w := newTabWriter()
	defer w.Flush()
	fmt.Fprintf(w, "名称\t%s\tid\t%s\n", l.Name, l.ID)
	fmt.Fprintf(w, "类型\t%s\t层级\t%d\n", l.Type, l.Index)
	fmt.Fprintf(w, "位置\t%.0f,%.0f\t尺寸\t%.0f×%.0f\n", l.X, l.Y, l.Width, l.Height)
	fmt.Fprintf(w, "旋转\t%.4g°\t不透明度\t%.4g\n", l.Rotation, l.Opacity)
	state := "可见"
	if !l.Visible {
		state = "隐藏"
	}
	if l.Locked {
		state += " · 锁定"
	}
	fmt.Fprintf(w, "状态\t%s\n", state)
	if l.Preview != "" {
		fmt.Fprintf(w, "内容\t%s\n", truncate(l.Preview, 120))
	}
}

// truncate cuts a preview down to n runes, so a long paragraph of copy cannot
// turn `layer show` into a dump.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// newTabWriter returns a tabwriter aligned to two-space gutters.
func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
}

// oneLineJSON renders a value as single-line JSON, for the last column of a table
// where a pretty-printed blob would break the alignment. An empty object reads as
// "nothing set", which is the honest answer for a preset with no style keys.
func oneLineJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	if string(b) == "{}" || string(b) == "null" {
		return "—"
	}
	return string(b)
}

func (c *Client) printProjects(list []server.ProjectSummary) {
	if c.JSON {
		printJSON(map[string]any{"projects": list})
		return
	}
	w := newTabWriter()
	defer w.Flush()
	fmt.Fprintln(w, "ID\t名称\t画布\t修订\t更新")
	for _, p := range list {
		fmt.Fprintf(w, "%s\t%s\t%d×%d\t%d\t%s\n", p.ID, p.Name, p.Width, p.Height, p.Revision,
			p.UpdatedAt.Local().Format("01-02 15:04"))
	}
}

func (c *Client) printLayers(list []server.LayerRow) {
	if c.JSON {
		printJSON(map[string]any{"layers": list})
		return
	}
	w := newTabWriter()
	defer w.Flush()
	fmt.Fprintln(w, "序\tID\t类型\t名称\t位置\t尺寸")
	for i, l := range list {
		vis := ""
		if !l.Visible {
			vis = "·"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s%s\t%.0f,%.0f\t%.0f×%.0f\n",
			i+1, l.ID, l.Type, l.Name, vis, l.X, l.Y, l.Width, l.Height)
	}
}

// --- flag helpers ---

// optFloat &c. return nil unless the flag was explicitly set, so a command map
// only ever carries the fields the caller actually asked to change.
func optFloat(cmd *cobra.Command, name string) *float64 {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	v, _ := cmd.Flags().GetFloat64(name)
	return &v
}

func optInt(cmd *cobra.Command, name string) *int {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	v, _ := cmd.Flags().GetInt(name)
	return &v
}

func optStr(cmd *cobra.Command, name string) *string {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	v, _ := cmd.Flags().GetString(name)
	return &v
}

func optBool(cmd *cobra.Command, name string) *bool {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	v, _ := cmd.Flags().GetBool(name)
	return &v
}

// assign writes v into m under key, skipping nil pointers.
func assign(m map[string]any, key string, v any) {
	switch t := v.(type) {
	case nil:
	case *float64:
		if t != nil {
			m[key] = *t
		}
	case *int:
		if t != nil {
			m[key] = *t
		}
	case *string:
		if t != nil && *t != "" {
			m[key] = *t
		}
	case *bool:
		if t != nil {
			m[key] = *t
		}
	default:
		m[key] = v
	}
}
