// Package cmd contains the launcher's Cobra commands.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

type exitStatus int

func (s exitStatus) Error() string { return fmt.Sprintf("exit status %d", s) }

type launcher struct {
	version string
	runApp  func(string, []string) int
}

// Execute invokes the Cobra command tree using the executable's original name.
func Execute(version string, argv []string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "missing executable name")
		return 2
	}
	app := &launcher{version: version}
	app.runApp = app.executeApp
	return app.execute(argv, runtime.GOOS)
}

func (app *launcher) execute(argv []string, goos string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "missing executable name")
		return 2
	}
	root := app.rootCommand()
	base := filepath.Base(argv[0])
	if goos == "windows" {
		base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	}
	args := argv[1:]
	// App links and unrecognised first words are catalogue commands. Translate
	// only this prefix; Cobra must never parse the application's arguments.
	if base != "idapps" {
		args = append([]string{"run", base}, args...)
	} else if len(args) > 0 {
		switch args[0] {
		case "help", "--help", "version", "list", "update", "run":
		default:
			args = append([]string{"run"}, args...)
		}
	}
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		if status, ok := err.(exitStatus); ok {
			return int(status)
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

func (app *launcher) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use: "idapps", Short: "Run developer tools in local Docker containers",
		SilenceErrors: true, SilenceUsage: true, DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, _ []string) { _ = cmd.Help() },
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		fmt.Fprintln(cmd.OutOrStdout(), "Usage: idapps update | list | version | [run] NAME [ARG...]\nRun idapps update before launching an application.")
	})
	root.SetHelpCommand(&cobra.Command{Use: "help", DisableFlagParsing: true, Run: func(cmd *cobra.Command, args []string) { root.HelpFunc()(cmd, args) }})
	root.AddCommand(app.runCommand(), app.listCommand(), app.updateCommand(), app.versionCommand())
	return root
}

func commandStatus(code int) error {
	if code != 0 {
		return exitStatus(code)
	}
	return nil
}
