// Package bundle finds what was shipped alongside this binary: the macOS app it
// sits in (if any), and the helpers built next to it.
package bundle

import (
	"os"
	"path/filepath"
	"runtime"
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

// Contents is where the app this binary was shipped in keeps its files: the
// .app's Contents directory on macOS, the install directory on Windows (this
// binary being resources\bin\davinci.exe next to resources\app.asar). "" when
// it runs from anywhere else.
func Contents() string {
	p := exe()
	if i := strings.Index(p, ".app/Contents/"); i >= 0 {
		return p[:i+len(".app/Contents")]
	}
	if runtime.GOOS == "windows" {
		bin := filepath.Dir(p)
		res := filepath.Dir(bin)
		if strings.EqualFold(filepath.Base(bin), "bin") && strings.EqualFold(filepath.Base(res), "resources") {
			if _, err := os.Stat(filepath.Join(res, "app.asar")); err == nil {
				return filepath.Dir(res)
			}
		}
	}
	return ""
}

// Node is the app's own executable, which runs a script as Node when
// ELECTRON_RUN_AS_NODE=1 is set; "" outside the app.
func Node() string {
	c := Contents()
	if c == "" {
		return ""
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(c, "davinci.exe")
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
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	t := filepath.Join(filepath.Dir(p), name)
	if info, err := os.Stat(t); err == nil && !info.IsDir() {
		return t
	}
	return ""
}
