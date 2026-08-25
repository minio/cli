package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/posener/complete"
)

// flagsToCompleteFlags transforms a cli.Flag to complete.Flags
// understood by posener/complete library.
func flagsToCompleteFlags(flags []Flag) complete.Flags {
	complFlags := make(complete.Flags)
	for _, f := range flags {
		for _, s := range strings.Split(f.GetName(), ",") {
			var flagName string
			s = strings.TrimSpace(s)
			if len(s) == 1 {
				flagName = "-" + s
			} else {
				flagName = "--" + s
			}
			complFlags[flagName] = f.GetPredictor()
		}
	}
	return complFlags
}

// cmdToCompleteCmd recursively transforms a Command (and its Subcommands) into
// a complete.Command understood by the posener/complete library. Hidden
// commands are skipped; aliases are registered alongside the primary name. The
// argument and flag-value predictors come from the command's own
// CustomCompletePredictor / CustomFlagPredictor fields.
func cmdToCompleteCmd(cmd Command) complete.Command {
	var complCmd complete.Command
	complCmd.Sub = make(complete.Commands)

	for _, subCmd := range cmd.Subcommands {
		if subCmd.Hidden {
			continue
		}
		cc := cmdToCompleteCmd(subCmd)
		complCmd.Sub[subCmd.Name] = cc
		for _, alias := range subCmd.Aliases {
			complCmd.Sub[alias] = cc
		}
	}

	complCmd.Args = cmd.CustomCompletePredictor
	complCmd.Flags = flagsToCompleteFlags(cmd.Flags)
	return complCmd
}

// shellCompleteCommand builds the root complete.Command for the application by
// walking its visible commands and global flags.
func (a *App) shellCompleteCommand() complete.Command {
	sub := make(complete.Commands)
	for _, cmd := range a.Commands {
		if cmd.Hidden {
			continue
		}
		cc := cmdToCompleteCmd(cmd)
		sub[cmd.Name] = cc
		for _, alias := range cmd.Aliases {
			sub[alias] = cc
		}
	}
	return complete.Command{
		Sub:         sub,
		GlobalFlags: flagsToCompleteFlags(a.GlobalFlags),
	}
}

// runShellCompletion answers a single shell-completion request described by the
// COMP_LINE / COMP_POINT environment variables and prints the predicted
// options. It is invoked from App.Run when EnableBashCompletion is set and the
// process was spawned by the shell for completion. The name passed to posener
// must match how the shell invoked the binary, hence filepath.Base(os.Args[0]).
func (a *App) runShellCompletion() {
	complete.New(filepath.Base(os.Args[0]), a.shellCompleteCommand()).Complete()
}
