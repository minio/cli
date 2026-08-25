package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	completeinstall "github.com/posener/complete/cmd/install"
)

// Errors returned by SetupShellCompletion so callers can switch on them to
// format their own user-facing messages. The framework itself prints nothing.
var (
	// ErrCompletionUnsupportedOS is returned when shell completion cannot be
	// installed on the current operating system (e.g. Windows).
	ErrCompletionUnsupportedOS = errors.New("shell completion is not supported on this operating system")
	// ErrCompletionUnsupportedShell is returned when the detected shell is not
	// one of the supported shells (bash, zsh, fish).
	ErrCompletionUnsupportedShell = errors.New("unsupported shell")
)

// supportedCompletionShells is the set of shells for which completion can be
// installed. posener/complete writes to whichever of these it finds present.
var supportedCompletionShells = map[string]bool{
	"bash": true,
	"zsh":  true,
	"fish": true,
}

// DetectShell reports the user's shell name (lowercased base name, e.g. "bash")
// and whether it was determined from the $SHELL environment variable. When
// $SHELL is unset it falls back to inspecting the parent process; fromEnv is
// false in that case. On Windows it returns early with fromEnv true and an
// empty name.
func DetectShell() (name string, fromEnv bool, err error) {
	shellName := os.Getenv("SHELL")
	if shellName != "" || runtime.GOOS == "windows" {
		return strings.ToLower(filepath.Base(shellName)), true, nil
	}

	ppid := os.Getppid()
	out, err := exec.Command("ps", "-p", strconv.Itoa(ppid), "-o", "comm=").Output()
	if err != nil {
		return "", false, err
	}
	shellName = strings.TrimSpace(string(out))
	return strings.ToLower(filepath.Base(shellName)), false, nil
}

// InstallShellCompletion registers cmd for shell completion in the user's shell
// rc files (bash/zsh/fish — whichever are present). It is a thin wrapper over
// posener/complete's installer.
func InstallShellCompletion(cmd string) error { return completeinstall.Install(cmd) }

// UninstallShellCompletion removes cmd's shell-completion registration.
func UninstallShellCompletion(cmd string) error { return completeinstall.Uninstall(cmd) }

// IsShellCompletionInstalled reports whether cmd's completion is already
// registered in any of the user's shell rc files.
func IsShellCompletionInstalled(cmd string) bool { return completeinstall.IsInstalled(cmd) }

// ShellCompletionResult describes the outcome of SetupShellCompletion so the
// caller can render an appropriate message.
type ShellCompletionResult struct {
	// Shell is the detected shell name (e.g. "bash").
	Shell string
	// DetectedFromEnv reports whether Shell came from $SHELL (vs. a fallback).
	DetectedFromEnv bool
	// AlreadyInstalled reports whether completion was already registered, in
	// which case no changes were made.
	AlreadyInstalled bool
}

// SetupShellCompletion is the one-call convenience that installs shell
// completion for cmd: it checks OS support, detects and validates the shell,
// and registers completion if not already present. It performs no output;
// callers inspect the returned result and error to present their own messages.
// Returns ErrCompletionUnsupportedOS or ErrCompletionUnsupportedShell for the
// respective unsupported cases.
func SetupShellCompletion(cmd string) (ShellCompletionResult, error) {
	var res ShellCompletionResult

	if runtime.GOOS == "windows" {
		return res, ErrCompletionUnsupportedOS
	}

	shell, fromEnv, err := DetectShell()
	if err != nil {
		return res, err
	}
	res.Shell = shell
	res.DetectedFromEnv = fromEnv

	if !supportedCompletionShells[shell] {
		return res, ErrCompletionUnsupportedShell
	}

	if IsShellCompletionInstalled(cmd) {
		res.AlreadyInstalled = true
		return res, nil
	}

	if err := InstallShellCompletion(cmd); err != nil {
		return res, err
	}
	return res, nil
}
