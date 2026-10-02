package cli

import (
	"os"
	"os/exec"
)

// execLookPath and execRun hide os/exec behind one import site, so every other
// CLI file stays free of process-spawning imports.
func execLookPath(bin string) (string, error) { return exec.LookPath(bin) }

func execRun(bin string, args ...string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
