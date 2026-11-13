// Command catalogue-compat validates the repository catalogue.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BabisK/idapps/internal/catalogue"
)

func main() {
	_, err := catalogue.ReadTree(filepath.Join("catalogue"), "999999.0.0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Catalogue descriptors and cross-app resources are valid.")
}
