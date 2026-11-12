package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/BabisK/idapps/internal/storage"
	"github.com/BabisK/idapps/internal/update"
	"github.com/spf13/cobra"
)

func (app *launcher) versionCommand() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Show launcher and catalogue versions", DisableFlagParsing: true,
		Args: noArgs("version"), RunE: func(_ *cobra.Command, _ []string) error { return commandStatus(app.showVersion()) }}
}

func (app *launcher) showVersion() int {
	paths, err := storage.Resolve()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 125
	}
	store, err := update.NewStore(paths, app.version)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 125
	}
	var commit string
	var cacheErr error
	err = store.WithLock(context.Background(), func() error {
		_, commit, cacheErr = store.Snapshot()
		return nil
	})
	fmt.Fprintf(os.Stdout, "idapps %s (schema 1)", app.version)
	if commit != "" {
		fmt.Fprintf(os.Stdout, " catalogue %s", commit)
	}
	if cacheErr != nil {
		fmt.Fprint(os.Stdout, " (catalogue unavailable)")
	}
	fmt.Fprintln(os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 125
	}
	return 0
}
