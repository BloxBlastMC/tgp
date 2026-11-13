// Command tgp-runner executes tgp CI jobs in Docker containers.
package main

import (
	"os"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/runnercmd"
)

func main() {
	os.Exit(runnercmd.Execute(os.Args[1:]))
}
