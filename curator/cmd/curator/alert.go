package main

import (
	"context"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	// alertTimeout bounds osascript so a stuck notification never holds up the exit.
	alertTimeout  = 5 * time.Second
	alertMaxRunes = 200
)

// alertFailure shows a macOS notification that `curator run` failed. launchd runs it unattended,
// so without this a failure only shows in ~/Library/Logs/changeloom-curator.log.
func alertFailure(err error) {
	if runtime.GOOS != "darwin" {
		return
	}
	msg := []rune(err.Error())
	if len(msg) > alertMaxRunes {
		msg = append(msg[:alertMaxRunes-1], '…')
	}
	ctx, cancel := context.WithTimeout(context.Background(), alertTimeout)
	defer cancel()
	// The message goes in as an argument, not into the script text, so quotes in it can't break it.
	cmd := exec.CommandContext(ctx, "osascript", //nolint:gosec // fixed binary; the message is argv, not script
		"-e", "on run argv",
		"-e", `display notification (item 1 of argv) with title "Changeloom curator" subtitle "run failed; see ~/Library/Logs/changeloom-curator.log"`,
		"-e", "end run",
		string(msg))
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Warn("could not show the failure notification", "err", err, "output", strings.TrimSpace(string(out)))
	}
}
