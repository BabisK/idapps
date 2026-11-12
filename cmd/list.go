package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/BabisK/idapps/internal/storage"
	"github.com/BabisK/idapps/internal/update"
	"github.com/spf13/cobra"
)

func (app *launcher) listCommand() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List available apps", DisableFlagParsing: true,
		Args: noArgs("list"), RunE: func(_ *cobra.Command, _ []string) error { return commandStatus(app.list()) }}
}

func (app *launcher) list() int {
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
	err = store.WithLock(context.Background(), func() error {
		c, _, err := store.Snapshot()
		_, records, recErr := store.Resources()
		if recErr != nil {
			return recErr
		}
		if c != nil {
			names := make([]string, 0, len(c.Apps))
			for name := range c.Apps {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				fmt.Fprintf(os.Stdout, "%s\t%s\n", name, c.Apps[name].Kind)
			}
		}
		var retired []string
		for name := range records {
			if c == nil {
				retired = append(retired, name)
			} else if _, ok := c.Apps[name]; !ok {
				retired = append(retired, name)
			}
		}
		sort.Strings(retired)
		for _, name := range retired {
			fmt.Fprintf(os.Stdout, "%s\tmanaged service (definition unavailable or retired)\n", name)
		}
		return err
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 125
	}
	return 0
}
