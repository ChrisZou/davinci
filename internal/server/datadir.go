package server

import (
	"os"
	"path/filepath"

	"davinci/internal/bundle"
)

// DefaultDataDir is where davinci keeps its database, assets and fonts when
// nothing says otherwise:
//
//  1. DAVINCI_DATA, if set;
//  2. for the davinci app (and the davinci command linked out of it),
//     ~/Library/Application Support/davinci;
//  3. the `data` directory of the davinci checkout the running binary belongs
//     to — <repo>/bin/davinci keeps its data in <repo>/data. Symlinks are
//     followed, so a `davinci` linked onto PATH still finds its checkout. This
//     is what makes every agent, run from whatever directory, reach the same
//     data;
//  4. `data` under the current directory.
//
// Nothing is ever written into the home folder by default. The CLI and the
// server both call this, so they always agree.
func DefaultDataDir() string {
	if v := os.Getenv("DAVINCI_DATA"); v != "" {
		return v
	}
	if bundle.Contents() != "" {
		if dir, err := os.UserConfigDir(); err == nil {
			return filepath.Join(dir, "davinci")
		}
	}
	if root := checkoutRoot(); root != "" {
		return filepath.Join(root, "data")
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "data")
	}
	return "data"
}

// checkoutRoot returns the davinci source checkout the running binary was
// built into (the binary sits in <root>/bin and <root>/go.mod names module
// davinci), or "" when it runs from anywhere else.
func checkoutRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	bin := filepath.Dir(exe)
	if filepath.Base(bin) != "bin" {
		return ""
	}
	root := filepath.Dir(bin)
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !isDavinciModule(mod) {
		return ""
	}
	return root
}

func isDavinciModule(gomod []byte) bool {
	for _, line := range splitLines(string(gomod)) {
		if line == "module davinci" {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, trimCR(s[start:i]))
			start = i + 1
		}
	}
	return append(out, trimCR(s[start:]))
}

func trimCR(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\r' {
		return s[:len(s)-1]
	}
	return s
}
