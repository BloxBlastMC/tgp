// Command tgp is the command line client for tgp servers.
package main

import (
	"os"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:]))
}
