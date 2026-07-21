package cli

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var (
	changeLogURL                    = "https://github.com/urfave/cli/blob/master/CHANGELOG.md"
	runAndExitOnErrorDeprecationURL = fmt.Sprintf("%s#deprecated-cli-app-runandexitonerror", changeLogURL)
)

// App is the main structure of a cli application. It is recommended that
// an app be created with the cli.NewApp() function
type App struct {
	// The name of the program. Defaults to path.Base(os.Args[0])
	Name string
	// Full name of command for help, defaults to Name
	HelpName string
	// Description of the program.
	Usage string
	// Text to override the USAGE section of help
	UsageText string
	// Description of the program argument format.
	ArgsUsage string
	// Version of the program
	Version string
	// Description of the program
	Description string
	// List of commands to execute
	Commands []Command
	// List of flags to parse
	Flags []Flag
	// Boolean to enable bash completion commands
	EnableBashCompletion bool
	// Boolean to hide built-in help flag
	HideHelp bool
	// Boolean to hide built-in help command
	HideHelpCommand bool
	// Boolean to hide built-in version flag and the VERSION section of help
	HideVersion bool
	// Populate on app startup, only gettable through method Categories()
	categories CommandCategories
	// An action to execute when the bash-completion flag is set
	BashComplete BashCompleteFunc
	// An action to execute before any subcommands are run, but after the context is ready
	// If a non-nil error is returned, no subcommands are run
	Before BeforeFunc
	// An action to execute after any subcommands are run, but after the subcommand has finished
	// It is run even if Action() panics
	After AfterFunc

	// The action to execute when no subcommands are specified
	Action ActionFunc

	// DefaultBefore executes before the app, commands, and subcommands that do
	// not specify a BeforeFunc of their own
	DefaultBefore BeforeFunc
	// DefaultAction is the action executed by the app, commands, and subcommands
	// that do not specify an action of their own
	DefaultAction ActionFunc
	// DefaultAfter executes after the app, commands, and subcommands that do not
	// specify an AfterFunc of their own
	DefaultAfter AfterFunc
	// DefaultOnUsageError is executed on a usage error by the app, commands, and
	// subcommands that do not specify an OnUsageError of their own
	DefaultOnUsageError OnUsageErrorFunc
	// GlobalHideHelp hides the help flag for the app, all commands, and all subcommands
	GlobalHideHelp bool
	// GlobalHideHelpCommand hides the help command for the app, all commands, and all subcommands
	GlobalHideHelpCommand bool
	// GlobalFlags are flags that can be used by the app, any command, or any
	// subcommand, unless command.NoGlobalFlags is true
	GlobalFlags []Flag

	// Execute this function if the proper command cannot be found
	CommandNotFound CommandNotFoundFunc
	// Execute this function if an usage error occurs
	OnUsageError OnUsageErrorFunc
	// Compilation date
	Compiled time.Time
	// List of all authors who contributed
	Authors []Author
	// Copyright of the binary if any
	Copyright string
	// Name of Author (Note: Use App.Authors, this is deprecated)
	Author string
	// Email of Author (Note: Use App.Authors, this is deprecated)
	Email string
	// Writer for standard output
	Writer io.Writer
	// HelpWriter for help output
	HelpWriter io.Writer
	// ErrWriter for error outptu
	ErrWriter io.Writer
	// Other custom info
	Metadata map[string]interface{}
	// Carries a function which returns app specific info.
	ExtraInfo func() map[string]string
	// CustomAppHelpTemplate the text template for app help topic.
	// cli.go uses text/template to render templates. You can
	// render custom help text by setting this variable.
	CustomAppHelpTemplate string

	didSetup bool
}

// Tries to find out when this binary was compiled.
// Returns the current time if it fails to find it.
func compileTime() time.Time {
	info, err := os.Stat(os.Args[0])
	if err != nil {
		return time.Now()
	}
	return info.ModTime()
}

// NewApp creates a new cli Application with some reasonable defaults for Name,
// Usage, Version and Action.
func NewApp() *App {
	return &App{
		Name:         filepath.Base(os.Args[0]),
		HelpName:     filepath.Base(os.Args[0]),
		Usage:        "A new cli application",
		UsageText:    "",
		Version:      "0.0.0",
		BashComplete: DefaultAppComplete,
		Action:       helpCommand.Action,
		Compiled:     compileTime(),
		Writer:       os.Stdout,
		HelpWriter:   os.Stdout,
	}
}

// Setup runs initialization code to ensure all data structures are ready for
// `Run` or inspection prior to `Run`.  It is internally called by `Run`, but
// will return early if setup has already happened.
func (a *App) Setup() {
	if a.didSetup {
		return
	}

	a.didSetup = true

	if a.Author != "" || a.Email != "" {
		a.Authors = append(a.Authors, Author{Name: a.Author, Email: a.Email})
	}

	newCmds := []Command{}
	for _, c := range a.Commands {
		if c.HelpName == "" {
			c.HelpName = fmt.Sprintf("%s %s", a.HelpName, c.Name)
		}
		newCmds = append(newCmds, c)
	}
	a.Commands = newCmds

	// make the app-wide GlobalFlags usable by the app itself
	for _, fl := range a.GlobalFlags {
		a.appendFlag(fl)
	}

	if a.Command(helpCommand.Name) == nil {
		if !a.hideHelpCommand() {
			a.Commands = append(a.Commands, helpCommand)
		}
		if !a.hideHelp() && (HelpFlag != BoolFlag{}) {
			a.appendFlag(HelpFlag)
		}
	}

	if !a.HideVersion {
		a.appendFlag(VersionFlag)
	}

	a.categories = CommandCategories{}
	for _, command := range a.Commands {
		a.categories = a.categories.AddCommand(command.Category, command)
	}
	sort.Sort(a.categories)

	if a.Metadata == nil {
		a.Metadata = make(map[string]interface{})
	}

	if a.Writer == nil {
		a.Writer = os.Stdout
	}
}

// Run is the entry point to the cli app. Parses the arguments slice and routes
// to the proper flag/args combination
func (a *App) Run(arguments []string) (err error) {
	a.Setup()

	// handle the completion flag separately from the flagset since
	// completion could be attempted after a flag, but before its value was put
	// on the command line. this causes the flagset to interpret the completion
	// flag name as the value of the flag before it which is undesirable
	// note that we can only do this because the shell autocomplete function
	// always appends the completion flag at the end of the command
	shellComplete, arguments := checkShellCompleteFlag(a, arguments)

	// parse flags
	set, err := flagSet(a.Name, a.Flags)
	if err != nil {
		return err
	}

	set.SetOutput(ioutil.Discard)
	err = set.Parse(arguments[1:])
	nerr := normalizeFlags(a.Flags, set)
	context := NewContext(a, set, nil)
	if nerr != nil {
		fmt.Fprintln(a.Writer, nerr)
		return nerr
	}
	context.shellComplete = shellComplete

	if checkCompletions(context) {
		return nil
	}

	if err != nil {
		if onUsageError := a.resolveOnUsageError(); onUsageError != nil {
			err := onUsageError(context, err, false)
			HandleExitCoder(err)
			return err
		}
		fmt.Fprintf(a.Writer, "%s %s\n\n", "Incorrect Usage.", err.Error())
		return err
	}

	if !a.hideHelp() && checkHelp(context) {
		ShowAppHelp(context)
		return nil
	}

	if !a.HideVersion && checkVersion(context) {
		ShowVersion(context)
		return nil
	}

	if after := a.resolveAfter(); after != nil {
		defer func() {
			if afterErr := after(context); afterErr != nil {
				if err != nil {
					err = NewMultiError(err, afterErr)
				} else {
					err = afterErr
				}
			}
		}()
	}

	if before := a.resolveBefore(); before != nil {
		beforeErr := before(context)
		if beforeErr != nil {
			fmt.Fprintf(a.Writer, "%v\n\n", beforeErr)
			HandleExitCoder(beforeErr)
			err = beforeErr
			return err
		}
	}

	args := context.Args()
	if args.Present() {
		name := args.First()
		c := a.Command(name)
		if c != nil {
			return c.Run(context)
		}
	}

	// Run default Action
	err = a.resolveAction()(context)

	HandleExitCoder(err)
	return err
}

// RunAndExitOnError calls .Run() and exits non-zero if an error was returned
//
// Deprecated: instead you should return an error that fulfills cli.ExitCoder
// to cli.App.Run. This will cause the application to exit with the given eror
// code in the cli.ExitCoder
func (a *App) RunAndExitOnError() {
	if err := a.Run(os.Args); err != nil {
		fmt.Fprintln(a.errWriter(), err)
		OsExiter(1)
	}
}

// RunAsSubcommand invokes the subcommand given the context, parses ctx.Args() to
// generate command-specific flags
func (a *App) RunAsSubcommand(ctx *Context) (err error) {
	// append help to commands
	if len(a.Commands) > 0 {
		if a.Command(helpCommand.Name) == nil {
			if !a.hideHelpCommand() {
				a.Commands = append(a.Commands, helpCommand)
			}
			if !a.hideHelp() && (HelpFlag != BoolFlag{}) {
				a.appendFlag(HelpFlag)
			}
		}
	}

	newCmds := []Command{}
	for _, c := range a.Commands {
		if c.HelpName == "" {
			c.HelpName = fmt.Sprintf("%s %s", a.HelpName, c.Name)
		}
		newCmds = append(newCmds, c)
	}
	a.Commands = newCmds

	// parse flags
	set, err := flagSet(a.Name, a.Flags)
	if err != nil {
		return err
	}

	set.SetOutput(ioutil.Discard)
	err = set.Parse(ctx.Args().Tail())
	nerr := normalizeFlags(a.Flags, set)
	context := NewContext(a, set, ctx)

	if nerr != nil {
		fmt.Fprintln(a.Writer, nerr)
		fmt.Fprintln(a.Writer)
		return nerr
	}

	if checkCompletions(context) {
		return nil
	}

	if err != nil {
		if onUsageError := a.resolveOnUsageError(); onUsageError != nil {
			err = onUsageError(context, err, true)
			HandleExitCoder(err)
			return err
		}
		fmt.Fprintf(a.Writer, "%s %s\n\n", "Incorrect Usage.", err.Error())
		return err
	}

	if len(a.Commands) > 0 {
		if checkSubcommandHelp(context) {
			return nil
		}
	} else {
		if checkCommandHelp(ctx, context.Args().First()) {
			return nil
		}
	}

	if after := a.resolveAfter(); after != nil {
		defer func() {
			afterErr := after(context)
			if afterErr != nil {
				HandleExitCoder(err)
				if err != nil {
					err = NewMultiError(err, afterErr)
				} else {
					err = afterErr
				}
			}
		}()
	}

	if before := a.resolveBefore(); before != nil {
		beforeErr := before(context)
		if beforeErr != nil {
			HandleExitCoder(beforeErr)
			err = beforeErr
			return err
		}
	}

	args := context.Args()
	if args.Present() {
		name := args.First()
		c := a.Command(name)
		if c != nil {
			return c.Run(context)
		}
	}

	// Run default Action
	err = a.resolveAction()(context)

	HandleExitCoder(err)
	return err
}

// Command returns the named command on App. Returns nil if the command does not exist
func (a *App) Command(name string) *Command {
	for _, c := range a.Commands {
		if c.HasName(name) {
			return &c
		}
	}

	return nil
}

// Categories returns a slice containing all the categories with the commands they contain
func (a *App) Categories() CommandCategories {
	return a.categories
}

// VisibleCategories returns a slice of categories and commands that are
// Hidden=false
func (a *App) VisibleCategories() []*CommandCategory {
	ret := []*CommandCategory{}
	for _, category := range a.categories {
		if visible := func() *CommandCategory {
			for _, command := range category.Commands {
				if !command.Hidden {
					return category
				}
			}
			return nil
		}(); visible != nil {
			ret = append(ret, visible)
		}
	}
	return ret
}

// VisibleCommands returns a slice of the Commands with Hidden=false
func (a *App) VisibleCommands() []Command {
	ret := []Command{}
	for _, command := range a.Commands {
		if !command.Hidden {
			ret = append(ret, command)
		}
	}
	return ret
}

// VisibleFlags returns a slice of the Flags with Hidden=false
func (a *App) VisibleFlags() []Flag {
	return visibleFlags(a.Flags)
}

func (a *App) hasFlag(flag Flag) bool {
	for _, f := range a.Flags {
		if flag == f {
			return true
		}
	}

	return false
}

func (a *App) errWriter() io.Writer {
	// When the app ErrWriter is nil use the package level one.
	if a.ErrWriter == nil {
		return ErrWriter
	}

	return a.ErrWriter
}

func (a *App) appendFlag(flag Flag) {
	if !a.hasFlag(flag) {
		a.Flags = append(a.Flags, flag)
	}
}

// hideHelp reports whether the built-in help flag should be hidden for this
// app, honoring both the app-specific HideHelp and the app-wide GlobalHideHelp.
func (a *App) hideHelp() bool {
	return a.GlobalHideHelp || a.HideHelp
}

// hideHelpCommand reports whether the built-in help command should be hidden
// for this app, honoring both the app-specific HideHelpCommand and the
// app-wide GlobalHideHelpCommand.
func (a *App) hideHelpCommand() bool {
	return a.GlobalHideHelpCommand || a.HideHelpCommand
}

func (a *App) resolveBefore() BeforeFunc {
	if a.Before != nil {
		return a.Before
	}
	return a.DefaultBefore
}

func (a *App) resolveAfter() AfterFunc {
	if a.After != nil {
		return a.After
	}
	return a.DefaultAfter
}

func (a *App) resolveAction() ActionFunc {
	if a.Action != nil {
		return a.Action
	}
	if a.DefaultAction != nil {
		return a.DefaultAction
	}
	return helpCommand.Action
}

func (a *App) resolveOnUsageError() OnUsageErrorFunc {
	if a.OnUsageError != nil {
		return a.OnUsageError
	}
	return a.DefaultOnUsageError
}

// Author represents someone who has contributed to a cli project.
type Author struct {
	Name  string // The Authors name
	Email string // The Authors email
}

// String makes Author comply to the Stringer interface, to allow an easy print in the templating process
func (a Author) String() string {
	e := ""
	if a.Email != "" {
		e = " <" + a.Email + ">"
	}

	return fmt.Sprintf("%v%v", a.Name, e)
}
