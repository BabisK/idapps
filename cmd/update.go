package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/BabisK/idapps/internal/storage"
	"github.com/BabisK/idapps/internal/update"
	"github.com/spf13/cobra"
)

func noArgs(name string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != 0 {
			return fmt.Errorf("%s does not accept arguments", name)
		}
		return nil
	}
}

func (app *launcher) updateCommand() *cobra.Command {
	return &cobra.Command{Use: "update", Short: "Update the application catalogue", DisableFlagParsing: true,
		Args: noArgs("update"), RunE: func(_ *cobra.Command, _ []string) error { return commandStatus(app.update()) }}
}

func (app *launcher) update() int {
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
	ctx := context.Background()
	generation, err := update.Fetch(ctx, app.version)
	var result update.Reconciliation
	if err == nil {
		var exe string
		exe, err = os.Executable()
		if err == nil {
			err = store.WithLock(ctx, func() error {
				var activateErr error
				result, activateErr = store.ActivateWithResult(generation, exe)
				return activateErr
			})
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 125
	}
	fmt.Fprintf(os.Stdout, "Updated catalogue %s (added: %d, removed: %d, repaired: %d, unchanged: %d)\n", generation.Commit, result.Added, result.Removed, result.Repaired, result.Unchanged)
	return 0
}
