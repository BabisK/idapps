package main

import (
	"os"

	"github.com/BabisK/idapps/cmd"
	buildversion "github.com/BabisK/idapps/internal/version"
)

var version string

func main() { os.Exit(cmd.Execute(buildversion.Current(version), os.Args)) }
