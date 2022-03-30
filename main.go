package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/elastic/elastic-agent-shipper/cmd"
)

var (
	// we don't have an easily available library, so the CLI system doesn't fully work right now.
	cfg = flag.String("cfg", "elastic-agent-shipper.yml", "path to the shipper config file")
)

func main() {
	command := cmd.NewCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

}
