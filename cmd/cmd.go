package cmd

import (
	// import logp flags
	// Elastic-agent currently imports into beats like this, I assume for license reasons?
	"flag"
	"fmt"
	"os"

	//import logp flags
	_ "github.com/elastic/elastic-agent-libs/logp/configure"
	"github.com/elastic/elastic-agent-shipper/server"
	"github.com/spf13/cobra"
)

// NewCommand returns a new command structure
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "elastic-agent-shipper [subcommand]",
	}
	cmd.PersistentFlags().AddGoFlag(flag.CommandLine.Lookup("path.config"))
	// logging flags
	cmd.PersistentFlags().AddGoFlag(flag.CommandLine.Lookup("v"))
	cmd.PersistentFlags().AddGoFlag(flag.CommandLine.Lookup("e"))
	cmd.PersistentFlags().AddGoFlag(flag.CommandLine.Lookup("d"))
	cmd.PersistentFlags().AddGoFlag(flag.CommandLine.Lookup("environment"))

	run := runCmd()
	cmd.Run = run.Run

	return cmd
}

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Start the elastic-agent-shipper.",
		Run: func(_ *cobra.Command, _ []string) {
			if err := server.LoadAndRun(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
				os.Exit(1)
			}
		},
	}
}
