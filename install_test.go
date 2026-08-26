package cli

import (
	"errors"
	"testing"

	multierror "github.com/hashicorp/go-multierror"
)

// withInstaller swaps in fn for the real installer for the test's duration.
// Never let the real one run here: it resolves the home dir via
// os/user.Current(), which ignores $HOME, so it would write into your
// actual shell rc files.
func withInstaller(t *testing.T, fn func(cmd string) error) {
	t.Helper()
	old := installer
	installer = fn
	t.Cleanup(func() { installer = old })
}

// alreadyInstalledErr mimics the *multierror.Error InstallShellCompletion
// returns when the given rc files already have completion registered.
func alreadyInstalledErr(rcFiles ...string) error {
	var err error
	for _, f := range rcFiles {
		err = multierror.Append(err, errors.New("already installed in "+f))
	}
	return err
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

// Regression test: SetupShellCompletion must recognize "already installed"
// for the shell it actually detected.
func TestSetupShellCompletionAlreadyInstalledForDetectedShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	withInstaller(t, func(cmd string) error {
		return alreadyInstalledErr("/home/user/.bashrc")
	})

	res, err := SetupShellCompletion("prog")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.AlreadyInstalled {
		t.Error("res.AlreadyInstalled = false, want true when bash's own rc file already has it")
	}
}

// The original bug: a stale .zshrc entry must not read as "already
// installed" when the detected shell is bash and bash's own install just
// succeeded silently (no error entry for it at all).
func TestSetupShellCompletionOtherShellAlreadyInstalledIsNotOurs(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	withInstaller(t, func(cmd string) error {
		return alreadyInstalledErr("/home/user/.zshrc")
	})

	res, err := SetupShellCompletion("prog")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.AlreadyInstalled {
		t.Error("res.AlreadyInstalled = true, want false: only zsh's config had a conflict, not bash's")
	}
}

// A non-aggregate install failure (e.g. no shells found) must propagate.
func TestSetupShellCompletionPropagatesUnrelatedError(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	wantErr := errors.New("Did not find any shells to install")
	withInstaller(t, func(cmd string) error { return wantErr })

	res, err := SetupShellCompletion("prog")
	if err != wantErr {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
	if res.AlreadyInstalled {
		t.Error("res.AlreadyInstalled = true, want false when install genuinely failed")
	}
}

// A genuine failure naming our own shell (not "already installed") must
// propagate too, not be swallowed as a benign conflict.
func TestSetupShellCompletionGenuineFailureForDetectedShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	withInstaller(t, func(cmd string) error {
		var err error
		err = multierror.Append(err, errors.New("open /home/user/.bashrc: permission denied"))
		return err
	})

	res, err := SetupShellCompletion("prog")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if res.AlreadyInstalled {
		t.Error("res.AlreadyInstalled = true, want false: this was a genuine failure, not an already-installed conflict")
	}
}

// Regression test: a cmd name that happens to contain another shell's name
// (e.g. "bash-tool") must not make fish's error get misread as bash's.
func TestSetupShellCompletionShellNameInCmdIsNotMisattributed(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	withInstaller(t, func(cmd string) error {
		return alreadyInstalledErr("/home/user/.config/fish/completions/" + cmd + ".fish")
	})

	res, err := SetupShellCompletion("bash-tool")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.AlreadyInstalled {
		t.Error("res.AlreadyInstalled = true, want false: the error was about fish's config, not bash's")
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
