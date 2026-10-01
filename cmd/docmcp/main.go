package main

import (
	"fmt"
	"os"

	"github.com/docmcp/docmcp/internal/app"
)

// version is stamped at build time:
//
//	go build -ldflags "-X main.version=v1.2.3"
var version = "dev"

func main() {
	root := app.NewRootCommand(version)
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "docmcp:", err)
		os.Exit(1)
	}
}
