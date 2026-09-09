package cli

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// withInstaller/withIsInstalled swap in fn for the real posener/complete
// call for the test's duration. Never let the real ones run here: they
// resolve the home dir via os/user.Current(), which ignores $HOME, so they'd
// touch your actual shell rc files.
func withInstaller(t *testing.T, fn func(cmd string) error) {
	t.Helper()
	old := installer
	installer = fn
	t.Cleanup(func() { installer = old })
}

func withIsInstalled(t *testing.T, fn func(cmd string) bool) {
	t.Helper()
	old := isInstalled
	isInstalled = fn
	t.Cleanup(func() { isInstalled = old })
}

func withShellIsInstalled(t *testing.T, fn func(shell, cmd string) bool) {
	t.Helper()
	old := shellIsInstalled
	shellIsInstalled = fn
	t.Cleanup(func() { shellIsInstalled = old })
}

// withFakeHome points the rc-file lookups at a temp directory and pins the
// binary path, so the shell config checks can be exercised without touching
// the real home directory.
func withFakeHome(t *testing.T, bin string) string {
	t.Helper()
	dir := t.TempDir()

	oldHome := homeDir
	homeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { homeDir = oldHome })

	oldBin := completionBinaryPath
	completionBinaryPath = func() (string, error) { return bin, nil }
	t.Cleanup(func() { completionBinaryPath = oldBin })

	// fishConfigDir consults XDG_CONFIG_HOME before falling back to
	// $home/.config; clear it so the fake home wins.
	t.Setenv("XDG_CONFIG_HOME", "")

	return dir
}

func TestSetupShellCompletionFreshInstall(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	withInstaller(t, func(cmd string) error { return nil })

	res, err := SetupShellCompletion("prog")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Shell != "bash" {
		t.Errorf("res.Shell = %q, want %q", res.Shell, "bash")
	}
	if res.AlreadyInstalled {
		t.Error("res.AlreadyInstalled = true, want false on a fresh install")
	}
}

// Regression test: SetupShellCompletion must not skip installing just
// because some *other* shell's config already had it — Install() always
// runs, and only a post-install disk check decides AlreadyInstalled.
func TestSetupShellCompletionAlreadyInstalled(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	withInstaller(t, func(cmd string) error {
		return errors.New("1 error occurred: * already installed in /home/user/.bashrc")
	})
	withShellIsInstalled(t, func(shell, cmd string) bool { return true })

	res, err := SetupShellCompletion("prog")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.AlreadyInstalled {
		t.Error("res.AlreadyInstalled = false, want true")
	}
}

// A genuine install failure (not just some shell already having it) must
// propagate: the post-install disk check finds nothing installed either.
func TestSetupShellCompletionPropagatesGenuineFailure(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	wantErr := errors.New("open /home/user/.bashrc: permission denied")
	withInstaller(t, func(cmd string) error { return wantErr })
	withShellIsInstalled(t, func(shell, cmd string) bool { return false })

	res, err := SetupShellCompletion("prog")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
	if res.AlreadyInstalled {
		t.Error("res.AlreadyInstalled = true, want false when install genuinely failed")
	}
}

func TestSetupShellCompletionUnsupportedShell(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/tcsh")
	// installer must not even be consulted for an unsupported shell.
	withInstaller(t, func(cmd string) error {
		t.Fatal("installer should not be called for an unsupported shell")
		return nil
	})

	_, err := SetupShellCompletion("prog")
	if !errors.Is(err, ErrCompletionUnsupportedShell) {
		t.Errorf("err = %v, want %v", err, ErrCompletionUnsupportedShell)
	}
}

// A failure to write the detected shell's config must propagate even when some
// *other* shell is already registered — the package-wide IsInstalled would
// report true there and hide the failure.
func TestSetupShellCompletionFailureForDetectedShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	wantErr := errors.New("open /home/user/.bashrc: permission denied")
	withInstaller(t, func(cmd string) error { return wantErr })
	// Some other shell (zsh, say) is registered...
	withIsInstalled(t, func(cmd string) bool { return true })
	// ...but bash, the shell actually in use, is not.
	withShellIsInstalled(t, func(shell, cmd string) bool { return shell != "bash" })

	res, err := SetupShellCompletion("prog")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
	if res.AlreadyInstalled {
		t.Error("res.AlreadyInstalled = true, want false when the detected shell's install failed")
	}
}

func TestShellCompletionInstalledBash(t *testing.T) {
	home := withFakeHome(t, "/usr/local/bin/prog")
	rc := ".bashrc"
	if runtime.GOOS == "darwin" {
		rc = ".bash_profile"
	}
	writeLines(t, filepath.Join(home, rc),
		"# some other config",
		"complete -C /usr/local/bin/prog prog",
	)

	if !shellCompletionInstalled("bash", "prog") {
		t.Error("shellCompletionInstalled(bash, prog) = false, want true")
	}
	if shellCompletionInstalled("bash", "other") {
		t.Error("shellCompletionInstalled(bash, other) = true, want false")
	}
	// zsh's rc file is absent, so it must not inherit bash's answer.
	if shellCompletionInstalled("zsh", "prog") {
		t.Error("shellCompletionInstalled(zsh, prog) = true, want false")
	}
}

func TestShellCompletionInstalledZsh(t *testing.T) {
	home := withFakeHome(t, "/usr/local/bin/prog")
	writeLines(t, filepath.Join(home, ".zshrc"),
		"autoload -U +X bashcompinit && bashcompinit",
		"complete -o nospace -C /usr/local/bin/prog prog",
	)

	if !shellCompletionInstalled("zsh", "prog") {
		t.Error("shellCompletionInstalled(zsh, prog) = false, want true")
	}
}

// zsh's line carries -o nospace; a bash-shaped line must not satisfy it.
func TestShellCompletionInstalledZshRejectsBashLine(t *testing.T) {
	home := withFakeHome(t, "/usr/local/bin/prog")
	writeLines(t, filepath.Join(home, ".zshrc"), "complete -C /usr/local/bin/prog prog")

	if shellCompletionInstalled("zsh", "prog") {
		t.Error("shellCompletionInstalled(zsh, prog) = true, want false for a bash-shaped line")
	}
}

func TestShellCompletionInstalledFish(t *testing.T) {
	home := withFakeHome(t, "/usr/local/bin/prog")
	dir := filepath.Join(home, ".config", "fish", "completions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLines(t, filepath.Join(dir, "prog.fish"), "function __complete_prog")

	if !shellCompletionInstalled("fish", "prog") {
		t.Error("shellCompletionInstalled(fish, prog) = false, want true")
	}
	if shellCompletionInstalled("fish", "other") {
		t.Error("shellCompletionInstalled(fish, other) = true, want false")
	}
}

// A different binary path means posener wrote the line for a different
// install, so it does not count as installed for this one.
func TestShellCompletionInstalledDifferentBinary(t *testing.T) {
	home := withFakeHome(t, "/opt/prog")
	rc := ".bashrc"
	if runtime.GOOS == "darwin" {
		rc = ".bash_profile"
	}
	writeLines(t, filepath.Join(home, rc), "complete -C /usr/local/bin/prog prog")

	if shellCompletionInstalled("bash", "prog") {
		t.Error("shellCompletionInstalled(bash, prog) = true, want false for a different binary path")
	}
}

func TestShellCompletionInstalledMissingConfig(t *testing.T) {
	withFakeHome(t, "/usr/local/bin/prog")

	for _, shell := range []string{"bash", "zsh", "fish", "tcsh"} {
		if shellCompletionInstalled(shell, "prog") {
			t.Errorf("shellCompletionInstalled(%s, prog) = true, want false with no config present", shell)
		}
	}
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
