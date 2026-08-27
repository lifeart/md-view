package main

import (
	"fmt"
	"os"
	"strings"
)

// prewarmFlag extracts `--prewarm <mode>` or `--prewarm=<mode>` from args.
func prewarmFlag(args []string) (string, bool) {
	for i, arg := range args {
		if mode, ok := strings.CutPrefix(arg, "--prewarm="); ok {
			return mode, true
		}
		if arg == "--prewarm" {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "status", true
		}
	}
	return "", false
}

// runPrewarmCommand applies the mode and returns the process exit code. It
// prints what actually happened rather than what was asked for: the system can
// refuse, and can also require the reader's approval before an agent runs, and
// reporting "on" in that case would be wrong.
func runPrewarmCommand(mode string) int {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "on", "install", "enable":
		if err := setPrewarm(true); err != nil {
			fmt.Fprintln(os.Stderr, "md-view: could not enable prewarm:", err)
			return 1
		}
	case "off", "remove", "disable":
		if err := setPrewarm(false); err != nil {
			fmt.Fprintln(os.Stderr, "md-view: could not disable prewarm:", err)
			return 1
		}
	case "status":
	default:
		fmt.Fprintf(os.Stderr, "md-view: unknown --prewarm mode %q (want on, off or status)\n", mode)
		return 2
	}
	fmt.Println(describePrewarm(prewarmState()))
	return 0
}

func describePrewarm(s PrewarmState) string {
	switch {
	case !s.Supported:
		return "prewarm: unsupported (needs macOS 13 or later)"
	case s.NeedsApproval:
		return "prewarm: registered, awaiting your approval in System Settings > General > Login Items"
	case s.Enabled:
		return "prewarm: on — MDv starts hidden at login"
	default:
		return "prewarm: off"
	}
}
