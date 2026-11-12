// Command catalogue-compat checks the current catalogue against release parsers.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BabisK/idapps/internal/compat"
)

func main() {
	repo, err := os.Getwd()
	if err == nil {
		err = compat.Check(repo, filepath.Join(repo, "catalogue.json"))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Catalogue is compatible with supported release parsers.")
}
