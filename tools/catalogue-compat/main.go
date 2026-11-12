// Command catalogue-compat checks the current catalogue against release parsers.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BabisK/idapps/internal/catalogue"
	"github.com/BabisK/idapps/internal/compat"
)

func main() {
	repo, err := os.Getwd()
	if err == nil {
		var data []byte
		data, err = catalogue.ReadTree(filepath.Join(repo, "catalogue"), "999999.0.0")
		if err == nil {
			var dir string
			dir, err = os.MkdirTemp("", "idapps-compat-")
			if err == nil {
				defer os.RemoveAll(dir)
				file := filepath.Join(dir, "catalogue.json")
				err = os.WriteFile(file, data, 0600)
				if err == nil {
					err = compat.Check(repo, file)
				}
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Catalogue is compatible with supported release parsers.")
}
