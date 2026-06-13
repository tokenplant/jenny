//go:build windows

package portal

import (
	"os"

	"golang.org/x/sys/windows"
)

// flock attempts to acquire an exclusive lock on a file using Windows LockFileEx.
// Returns the file handle and nil on success, or nil and an error on failure.
// The lock is released when the file is closed.
func flock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}

	// Use LockFileEx for Windows byte-range exclusive locking via golang.org/x/sys/windows
	var ol windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
