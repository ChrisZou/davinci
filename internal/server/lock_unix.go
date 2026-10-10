//go:build !windows

package server

import (
	"os"
	"syscall"
)

// tryLock takes an exclusive lock on f without waiting; it fails if another
// process holds one.
func tryLock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
