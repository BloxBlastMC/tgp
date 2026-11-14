// Command tgp-server runs the tgp git platform server.
package main

import (
	"os"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/servercmd"
)

func main() {
	os.Exit(servercmd.Execute(os.Args[1:]))
}
