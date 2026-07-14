// Command devps lists every dev server you left running — port, project
// directory, git branch, age — by joining listening sockets to the
// processes, working directories, and repositories behind them.
package main

import (
	"os"

	"github.com/JaydenCJ/devps/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
