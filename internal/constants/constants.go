// Package constants holds project-wide configuration.
package constants

import (
	"os"
	"path/filepath"
)

// ProjectName is the canonical name of the binary.
const ProjectName = "jenny"

// Version is the current version of jenny.
const Version = "0.1.0"

// JennyHomeDirFunc is the function that returns the jenny home directory.
// It can be overridden in tests.
var JennyHomeDirFunc = func() string {
	dirName := "." + ProjectName
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return dirName
	}
	return filepath.Join(home, dirName)
}

// JennyHomeDir returns the user's jenny home directory (~/.jenny).
// If os.UserHomeDir() fails, it falls back to a ".jenny" directory in the
// current working directory.
func JennyHomeDir() string {
	return JennyHomeDirFunc()
}

// DefaultTranscriptDir returns the default transcript directory path.
func DefaultTranscriptDir() string {
	return filepath.Join(JennyHomeDir(), "transcripts")
}

// ScratchpadDir returns the scratchpad directory path (~/.jenny/scratchpad).
// The directory is created lazily by tools on first access.
func ScratchpadDir() string {
	return filepath.Join(JennyHomeDir(), "scratchpad")
}

// MaxTombstoneRewriteBytes is the maximum file size (50 MiB) before a tombstone
// rewrite or full rewrite operation is refused to prevent OOM.
const MaxTombstoneRewriteBytes = 50 * 1024 * 1024
