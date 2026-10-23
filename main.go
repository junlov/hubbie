// Command hubbie gives each client its own login for the HubSpot CLI.
package main

import (
	"os"

	"github.com/junlov/hubbie/internal/cli"
)

func main() {
	os.Exit(cli.New().Run(os.Args[1:]))
}
