package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// Native image clipboard adapters. Shell text never interpolates paths; the
// PNG path is passed as an argument (or its contents through stdin).
func copyImage(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("osascript", "-e", "on run argv\nset the clipboard to (read (POSIX file (item 1 of argv)) as «class PNGf»)\nend run", path)
	case "windows":
		cmd = exec.Command("powershell.exe", "-NoProfile", "-STA", "-Command", `$ErrorActionPreference = 'Stop'; Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; $image = [System.Drawing.Image]::FromFile($env:DAVINCI_CLIPBOARD_IMAGE); try { [System.Windows.Forms.Clipboard]::SetImage($image) } finally { $image.Dispose() }`)
		cmd.Env = append(os.Environ(), "DAVINCI_CLIPBOARD_IMAGE="+path)
	default:
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			cmd = exec.Command("wl-copy", "--type", "image/png")
		} else {
			cmd = exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-i")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		cmd.Stdin = f
	}
	// Linux clipboard owners may fork and keep descriptors open. Avoid pipe
	// capture here so waiting for the foreground command cannot hang on them.
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("复制图片失败（Linux 需要 wl-copy 或 xclip）: %w", err)
	}
	return nil
}
