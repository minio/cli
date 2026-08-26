package cli

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/posener/complete"
)

// newCompletionTestApp builds an app exercising every completion path:
// commands with aliases, a hidden command, a subcommand tree (with a hidden
// child), a leaf command carrying an arg predictor, and a command with a
// flag-value predictor. Help/version are hidden to keep predictions clean.
func newCompletionTestApp() *App {
	app := NewApp()
	app.Name = "prog"
	app.HideHelp = true
	app.HideHelpCommand = true
	app.HideVersion = true
	app.EnableBashCompletion = true
	app.Commands = []Command{
		{
			Name:    "widget",
			Aliases: []string{"w"},
			Subcommands: Commands{
				{Name: "make"},
				{Name: "list"},
				{Name: "internal", Hidden: true},
			},
		},
		{
			Name:                    "pick",
			CustomCompletePredictor: complete.PredictSet("alpha", "beta"),
		},
		{
			Name: "paint",
			Flags: []Flag{StringFlag{
				Name:                "color",
				CustomFlagPredictor: complete.PredictSet("green", "red")},
			},
		},
		{
			Name:   "secret",
			Hidden: true,
		},
	}
	return app
}

// completeLine drives a single COMP_LINE completion request through app.Run and
// returns the predicted options, sorted, capturing what posener writes to
// stdout.
func completeLine(t *testing.T, app *App, line string) []string {
	t.Helper()

	if err := os.Setenv("COMP_LINE", line); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("COMP_POINT", strconv.Itoa(len(line))); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("COMP_LINE")
	defer os.Unsetenv("COMP_POINT")

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	runErr := app.Run([]string{"prog"})

	w.Close()
	os.Stdout = old

	if runErr != nil {
		t.Fatalf("Run returned error during completion: %v", runErr)
	}

	var out []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if s := strings.TrimSpace(sc.Text()); s != "" {
			out = append(out, s)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestShellCompletion(t *testing.T) {
	app := newCompletionTestApp()

	cases := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "top-level command names, hidden excluded, aliases included",
			line: "prog ",
			want: []string{"paint", "pick", "w", "widget"},
		},
		{
			name: "subcommand recursion, hidden child excluded",
			line: "prog widget ",
			want: []string{"list", "make"},
		},
		{
			name: "alias resolves to same subcommands",
			line: "prog w ",
			want: []string{"list", "make"},
		},
		{
			name: "arg predictor on leaf command",
			line: "prog pick ",
			want: []string{"alpha", "beta"},
		},
		{
			name: "arg predictor honors prefix",
			line: "prog pick a",
			want: []string{"alpha"},
		},
		{
			name: "flag name completion",
			line: "prog paint -",
			want: []string{"--color"},
		},
		{
			name: "flag value prediction",
			line: "prog paint --color ",
			want: []string{"green", "red"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := completeLine(t, app, tc.line)
			if !eqStrings(got, tc.want) {
				t.Errorf("line %q: got %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

// TestShellCompletionDisabled verifies that without EnableBashCompletion the
// COMP_LINE path is not taken.
func TestShellCompletionDisabled(t *testing.T) {
	app := newCompletionTestApp()
	app.EnableBashCompletion = false
	// No-op action so the normal (non-completion) path produces no output.
	app.Action = func(*Context) error { return nil }

	got := completeLine(t, app, "prog ")
	if len(got) != 0 {
		t.Errorf("expected no completion output when disabled, got %v", got)
	}
}

// newGlobalFlagsTestApp builds an app with an app-wide GlobalFlags predictor
// and two commands: one that inherits global flags normally, and one with
// NoGlobalFlags set.
func newGlobalFlagsTestApp() *App {
	app := NewApp()
	app.Name = "prog"
	app.HideHelp = true
	app.HideHelpCommand = true
	app.HideVersion = true
	app.EnableBashCompletion = true
	app.GlobalFlags = []Flag{StringFlag{
		Name:                "profile",
		CustomFlagPredictor: complete.PredictSet("dev", "prod"),
	}}
	app.Commands = []Command{
		{Name: "child"},
	}
	return app
}

// TestGlobalFlagsPropagateToChildCommands is a regression test for
// cmdToCompleteCmd not propagating App.GlobalFlags into child
// complete.Command trees. It should predict the global "--profile" flag's
// values inside a child command, unless the child sets NoGlobalFlags.
func TestGlobalFlagsPropagateToChildCommands(t *testing.T) {
	app := newGlobalFlagsTestApp()

	cases := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "global flag value predicted inside child command",
			line: "prog child --profile ",
			want: []string{"dev", "prod"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := completeLine(t, app, tc.line)
			if !eqStrings(got, tc.want) {
				t.Errorf("line %q: got %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

// newHiddenAliasesTestApp builds an app with a top-level command and a nested
// subcommand, both carrying HiddenAliases: true.
func newHiddenAliasesTestApp() *App {
	app := NewApp()
	app.Name = "prog"
	app.HideHelp = true
	app.HideHelpCommand = true
	app.HideVersion = true
	app.EnableBashCompletion = true
	app.Commands = []Command{
		{
			Name:          "widget",
			Aliases:       []string{"w"},
			HiddenAliases: true,
			Subcommands: Commands{
				{
					Name:          "make",
					Aliases:       []string{"m"},
					HiddenAliases: true,
				},
			},
		},
	}
	return app
}

// TestHiddenAliasesExcludedFromCompletion is a regression test for both
// alias-registration loops in cmdToCompleteCmd ignoring HiddenAliases: they
// currently register aliases regardless of the flag.
func TestHiddenAliasesExcludedFromCompletion(t *testing.T) {
	app := newHiddenAliasesTestApp()

	cases := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "top-level hidden alias omitted",
			line: "prog ",
			want: []string{"widget"},
		},
		{
			name: "nested hidden alias omitted",
			line: "prog widget ",
			want: []string{"make"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := completeLine(t, app, tc.line)
			if !eqStrings(got, tc.want) {
				t.Errorf("line %q: got %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}
