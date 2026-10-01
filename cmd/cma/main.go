//go:build linux

package main

import (
	"os"

	"claude-multi-account/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:])) }
