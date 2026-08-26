package cli

import (
	"errors"
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
	withIsInstalled(t, func(cmd string) bool { return true })

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
	withIsInstalled(t, func(cmd string) bool { return false })

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
