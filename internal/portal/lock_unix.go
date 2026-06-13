//go:build unix

package portal

import (
	"os"
	"syscall"
)

// flock attempts to acquire an exclusive lock on a file.
// Returns the file handle and nil on success, or nil and an error on failure.
// The lock is released when the file is closed.
func flock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
