package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
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

// installer, isInstalled and shellIsInstalled are seams over the real
// installation checks, for tests.
var (
	installer        = completeinstall.Install
	isInstalled      = completeinstall.IsInstalled
	shellIsInstalled = shellCompletionInstalled
)

// InstallShellCompletion registers cmd for shell completion in the user's shell
// rc files (bash/zsh/fish — whichever are present). It is a thin wrapper over
// posener/complete's installer.
func InstallShellCompletion(cmd string) error { return installer(cmd) }

// UninstallShellCompletion removes cmd's shell-completion registration.
func UninstallShellCompletion(cmd string) error { return completeinstall.Uninstall(cmd) }

// IsShellCompletionInstalled reports whether cmd's completion is already
// registered in any of the user's shell rc files.
func IsShellCompletionInstalled(cmd string) bool { return isInstalled(cmd) }

// ShellCompletionResult describes the outcome of SetupShellCompletion so the
// caller can render an appropriate message.
type ShellCompletionResult struct {
	// Shell is the detected shell name (e.g. "bash").
	Shell string
	// DetectedFromEnv reports whether Shell came from $SHELL (vs. a fallback).
	DetectedFromEnv bool
	// AlreadyInstalled reports whether completion was already registered for
	// Shell specifically, in which case no changes were made to its config.
	AlreadyInstalled bool
}

// SetupShellCompletion is the one-call convenience that installs shell
// completion for cmd: it checks OS support, detects and validates the shell,
// and registers completion. It performs no output; callers inspect the
// returned result and error to present their own messages. Returns
// ErrCompletionUnsupportedOS or ErrCompletionUnsupportedShell for the
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

	if installErr := InstallShellCompletion(cmd); installErr != nil {
		// Install() writes every shell config it finds and reports a
		// combined error, so a failure here may belong to a shell the user
		// does not run. Re-check the detected shell's own config on disk:
		// only if that one is registered is the error harmless.
		if !shellIsInstalled(shell, cmd) {
			return res, installErr
		}
		res.AlreadyInstalled = true
	}
	return res, nil
}

// The helpers below mirror the per-shell logic in posener/complete's
// cmd/install package, which exports only a package-wide IsInstalled that
// answers "is this registered for *any* shell". SetupShellCompletion needs the
// per-shell answer. The rc-file candidates and the completion lines must stay
// byte-identical to that package's, or the check reports "not installed" for a
// config it wrote itself.

// homeDir and completionBinaryPath are seams over the two pieces of ambient
// state the checks below depend on, for tests.
var (
	homeDir              = userHomeDir
	completionBinaryPath = executablePath
)

// userHomeDir resolves the home directory through os/user, as posener does.
func userHomeDir() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}

// executablePath returns the absolute path to the running executable.
func executablePath() (string, error) {
	bin, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(bin)
}

// shellCompletionInstalled reports whether cmd's completion is registered in
// the given shell's own configuration.
func shellCompletionInstalled(shell, cmd string) bool {
	bin, err := completionBinaryPath()
	if err != nil {
		return false
	}

	switch shell {
	case "bash":
		rc := bashRCFile()
		return rc != "" && lineInFile(rc, fmt.Sprintf("complete -C %s %s", bin, cmd))
	case "zsh":
		rc := rcFile(".zshrc")
		return rc != "" && lineInFile(rc, fmt.Sprintf("complete -o nospace -C %s %s", bin, cmd))
	case "fish":
		dir := fishConfigDir()
		if dir == "" {
			return false
		}
		_, err := os.Stat(filepath.Join(dir, "completions", fmt.Sprintf("%s.fish", cmd)))
		return err == nil
	}
	return false
}

// bashRCFile returns the first existing bash config file posener would have
// installed into, or "" if none exist.
func bashRCFile() string {
	candidates := []string{".bashrc", ".bash_profile", ".bash_login", ".profile"}
	if runtime.GOOS == "darwin" {
		candidates = []string{".bash_profile"}
	}
	for _, name := range candidates {
		if f := rcFile(name); f != "" {
			return f
		}
	}
	return ""
}

// rcFile returns the path to name under the user's home directory if it
// exists, or "" otherwise.
func rcFile(name string) string {
	home, err := homeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, name)
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// fishConfigDir returns the user's fish configuration directory if it exists,
// or "" otherwise.
func fishConfigDir() string {
	home, err := homeDir()
	if err != nil {
		return ""
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	dir := filepath.Join(configHome, "fish")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// lineInFile reports whether name contains a line exactly equal to lookFor.
func lineInFile(name, lookFor string) bool {
	f, err := os.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if scanner.Text() == lookFor {
			return true
		}
	}
	return false
}
