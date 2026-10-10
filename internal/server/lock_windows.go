//go:build windows

package server

import (
	"os"

	"golang.org/x/sys/windows"
)

// tryLock takes an exclusive lock on f without waiting; it fails if another
// process holds one.
func tryLock(f *os.File) error {
	var ol windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)
}
