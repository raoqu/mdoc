package main

import (
	"context"
	_ "embed"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

//go:embed browser_activate_macos.js
var activateBrowserScript string

func openBrowser(target string) error {
	if runtime.GOOS == "darwin" && activateBrowserTab(target) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.CommandContext(ctx, "open", target)
	case "windows":
		command = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.CommandContext(ctx, "xdg-open", target)
	}
	// Wait for the launcher even when this is a second launch about to exit.
	return command.Run()
}

func activateBrowserTab(target string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", activateBrowserScript, target).Output()
	return err == nil && strings.TrimSpace(string(output)) == "activated"
}
