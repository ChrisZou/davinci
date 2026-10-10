// Package bundle finds what was shipped alongside this binary: the macOS app it
// sits in (if any), and the helpers built next to it.
package bundle

import (
	"os"
	"path/filepath"
	"strings"
)

// exe is the running binary's real path: the davinci command on PATH is a
// symlink into the app (or into a checkout's bin/).
func exe() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return p
}

// Contents is the Contents directory of the .app this binary was shipped in,
// or "" when it runs from anywhere else.
func Contents() string {
	p := exe()
	i := strings.Index(p, ".app/Contents/")
	if i < 0 {
		return ""
	}
	return p[:i+len(".app/Contents")]
}

// Node is the app's own executable, which runs a script as Node when
// ELECTRON_RUN_AS_NODE=1 is set; "" outside the app.
func Node() string {
	c := Contents()
	if c == "" {
		return ""
	}
	entries, err := os.ReadDir(filepath.Join(c, "MacOS"))
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			return filepath.Join(c, "MacOS", e.Name())
		}
	}
	return ""
}

// Tool is a helper built next to this binary (bin/<name> in a checkout,
// Resources/bin/<name> in the app), or "" when there is none.
func Tool(name string) string {
	p := exe()
	if p == "" {
		return ""
	}
	t := filepath.Join(filepath.Dir(p), name)
	if info, err := os.Stat(t); err == nil && !info.IsDir() {
		return t
	}
	return ""
}
