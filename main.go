package main

import (
	"os"

	peggplugin "github.com/peggco/pegg/pkg/plugin"
)

func main() {
	if len(os.Args) > 1 && os.Getenv("PEGG_PLUGIN") == "" {
		os.Exit(runCLI(os.Args[1:]))
	}
	peggplugin.Serve(&Plugin{})
}
