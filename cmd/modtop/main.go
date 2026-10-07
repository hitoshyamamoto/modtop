// Command modtop is an interactive terminal viewer for Modbus devices.
package main

import (
	"fmt"
	"os"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	fmt.Fprintf(os.Stderr, "modtop %s: under development\n", version)
	os.Exit(1)
}
