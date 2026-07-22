// Package gitcmd wraps git subprocess invocations.
package gitcmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// gitEnv disables interactive credential/terminal prompts so a private or
// auth-required remote fails fast instead of hanging the server.
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=", // no askpass helper
		"GCM_INTERACTIVE=Never",
	)
}

// Run executes `git -C dir <args...>` and returns trimmed stdout.
// dir may be "" to run in the current working directory.
func Run(dir string, args ...string) (string, error) {
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", full...)
	cmd.Env = gitEnv()
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// RunStream executes git and returns raw stdout bytes (no trimming),
// suitable for large outputs like `git log`.
func RunStream(dir string, args ...string) ([]byte, error) {
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", full...)
	cmd.Env = gitEnv()
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}
