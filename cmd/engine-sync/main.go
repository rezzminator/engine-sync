// Command engine-sync compiles Claude sources into native target mirrors.
package main

import (
	"github.com/rezzminator/engine-sync/internal/cli"
	"os"
)

var version = "0.1.0"

func main() { os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr, os.Stdin, version)) }
