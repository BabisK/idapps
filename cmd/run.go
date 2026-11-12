package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/BabisK/idapps/internal/catalogue"
	"github.com/BabisK/idapps/internal/engine"
	"github.com/BabisK/idapps/internal/storage"
	"github.com/BabisK/idapps/internal/update"
	"github.com/spf13/cobra"
)

var followServiceLogs = engine.ServiceLogs

func (app *launcher) runCommand() *cobra.Command {
	return &cobra.Command{Use: "run NAME [ARG...]", Short: "Run a catalogue app", DisableFlagParsing: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("run requires an application name")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error { return commandStatus(app.runApp(args[0], args[1:])) },
	}
}

func (app *launcher) executeApp(name string, args []string) int {
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
	var definition catalogue.App
	var ranService, streamLogs bool
	status := 0
	err = store.WithLock(ctx, func() error {
		c, _, err := store.Snapshot()
		_, records, recErr := store.Resources()
		if recErr != nil {
			return recErr
		}
		var found bool
		if c != nil {
			definition, found = c.Apps[name]
		}
		_, recorded := records[name]
		manage := len(args) > 0 && (args[0] == "stop" || args[0] == "logs" || args[0] == "clean")
		if recorded && manage && (!found || definition.Kind == "service") {
			if args[0] == "logs" {
				streamLogs = true
				return nil
			}
			ranService = true
			var current *catalogue.App
			if found {
				current = &definition
			}
			return engine.Service(ctx, store, name, current, args)
		}
		if err != nil {
			return err
		}
		if !found {
			status = 2
			return fmt.Errorf("unknown app %q", name)
		}
		if definition.Kind == "service" {
			if len(args) > 0 && args[0] == "logs" {
				streamLogs = true
				return nil
			}
			ranService = true
			return engine.Service(ctx, store, name, &definition, args)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, engine.ErrUsage) {
			return 2
		}
		if status != 0 {
			return status
		}
		return 125
	}
	if streamLogs {
		if err := followServiceLogs(ctx, store, name, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			if errors.Is(err, engine.ErrUsage) {
				return 2
			}
			return 125
		}
		return 0
	}
	if !ranService {
		code, err := engine.RunCommand(ctx, definition, args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return code
	}
	return 0
}
