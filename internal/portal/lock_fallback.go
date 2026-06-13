//go:build !unix && !windows

package portal

import (
	"fmt"
	"os"
)

// flock is a fallback implementation for platforms without syscall.Flock or LockFileEx.
// It uses O_EXCL|O_CREATE semantics for atomic create-or-fail locking.
// Returns the file handle and nil on success, or nil and an error on failure.
func flock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("portal already running")
		}
		return nil, err
	}
	// We got the exclusive lock via O_EXCL - close immediately but keep the file as "locked"
	// The existence of the file serves as the lock indicator
	f.Close()
	return f, nil
}
