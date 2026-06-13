package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/portal"
)

// isInteractive returns true if stdout is connected to a terminal.
func isInteractive() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// tryOpenBrowser attempts to open the URL in the default browser.
func tryOpenBrowser(url string) {
	cmds := []string{"open", "xdg-open"}
	for _, cmd := range cmds {
		if _, err := exec.LookPath(cmd); err == nil {
			exec.Command(cmd, url).Start()
			return
		}
	}
	log.Print("Warning: could not auto-open browser. Open manually:")
	log.Print("  " + url)
}

// runPortal is the main function for the portal command.
func runPortal(ctx context.Context) error {
	p, err := portal.Start(ctx, constants.JennyHomeDir())
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://127.0.0.1:%d?token=%s", p.Port(), p.AuthToken())

	// Write URL to file for non-interactive (GUI double-click) mode
	if !isInteractive() {
		if err := p.WritePortalURLFile(); err != nil {
			log.Printf("Warning: could not write portal URL file: %v", err)
		}

		// On macOS, show a dialog with the URL
		if runtime.GOOS == "darwin" {
			if _, err := exec.LookPath("osascript"); err == nil {
				script := fmt.Sprintf(`display dialog "Jenny Portal started" & return & "%s" buttons {"Open"} default button "Open"`, url)
				cmd := exec.Command("osascript", "-e", script)
				cmd.Start()
			}
		}
	} else {
		// Interactive terminal mode: auto-open browser and print URL
		tryOpenBrowser(url)
		fmt.Printf("Portal started at http://127.0.0.1:%d\n", p.Port())
		fmt.Printf("Auth token: %s\n", p.AuthToken())
	}

	// Block on signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
	case <-ctx.Done():
	}

	return p.Shutdown(context.Background())
}