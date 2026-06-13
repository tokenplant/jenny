// Package constants holds project-wide configuration.
package constants

import (
	"os"
	"path/filepath"
)

// ProjectName is the canonical name of the binary.
const ProjectName = "jenny"

// ProjectDirName is the project-local directory name (derived from ProjectName).
const ProjectDirName = "." + ProjectName // ".jenny"

// PluginDirName is the plugin marker directory name.
const PluginDirName = "." + ProjectName + "-plugin" // ".jenny-plugin"

// IgnoreFileName is the jenny-specific ignore file name.
const IgnoreFileName = "." + ProjectName + "ignore" // ".jennyignore"

// ProjectJennyDir returns the project-local .jenny directory path for the given cwd.
func ProjectJennyDir(cwd string) string {
	return filepath.Join(cwd, ProjectDirName)
}

// Version is the current version of jenny. Overridable at build time via
// `-ldflags '-X github.com/ipy/jenny/internal/constants.Version=<value>'`.
var Version = "0.3.0"

// JennyHomeDirFunc is the function that returns the jenny home directory.
// It can be overridden in tests.
var JennyHomeDirFunc = func() string {
	if h := os.Getenv("JENNY_HOME"); h != "" {
		return h
	}
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

// SessionDir returns the directory for a specific session (~/.jenny/sessions/<sessionID>).
func SessionDir(sessionID string) string {
	if sessionID == "" {
		return JennyHomeDir()
	}
	return filepath.Join(JennyHomeDir(), "sessions", sessionID)
}

// ScratchpadDir returns the scratchpad directory path.
// If sessionID is provided, it returns a session-specific scratchpad (~/.jenny/sessions/<sessionID>/scratchpad).
func ScratchpadDir(sessionID ...string) string {
	if len(sessionID) > 0 && sessionID[0] != "" {
		return filepath.Join(SessionDir(sessionID[0]), "scratchpad")
	}
	return filepath.Join(JennyHomeDir(), "scratchpad")
}

// SpillsDir returns the spills directory path.
// If sessionID is provided, it returns a session-specific spills directory (~/.jenny/sessions/<sessionID>/spills).
func SpillsDir(sessionID ...string) string {
	if len(sessionID) > 0 && sessionID[0] != "" {
		return filepath.Join(SessionDir(sessionID[0]), "spills")
	}
	return filepath.Join(JennyHomeDir(), "spills")
}

// MaxTombstoneRewriteBytes is the maximum file size (50 MiB) before a tombstone
// rewrite or full rewrite operation is refused to prevent OOM.
const MaxTombstoneRewriteBytes = 50 * 1024 * 1024
