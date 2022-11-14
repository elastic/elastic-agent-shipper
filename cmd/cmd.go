// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package cmd

import (
	// import logp flags
	// Elastic-agent currently imports into beats like this, I assume for license reasons?
	"flag"
	"fmt"
	"os"

	//import logp flags
	"github.com/spf13/cobra"

	_ "github.com/elastic/elastic-agent-libs/logp/configure"
	"github.com/elastic/elastic-agent-shipper/controller"
)

// NewCommand returns a new command structure
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "elastic-agent-shipper [subcommand]",
	}
	// logging flags
	cmd.PersistentFlags().AddGoFlag(flag.CommandLine.Lookup("v"))
	cmd.PersistentFlags().AddGoFlag(flag.CommandLine.Lookup("e"))
	cmd.PersistentFlags().AddGoFlag(flag.CommandLine.Lookup("d"))
	cmd.PersistentFlags().AddGoFlag(flag.CommandLine.Lookup("environment"))
	cmd.PersistentFlags().AddGoFlag(flag.CommandLine.Lookup("c"))

	run := runCmd()

	cmd.AddCommand(newVersionCommand())
	cmd.AddCommand(run)
	cmd.Run = run.Run

	return cmd
}

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Start the elastic-agent-shipper.",
		Run: func(_ *cobra.Command, _ []string) {
			if err := controller.LoadAndRun(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
				os.Exit(1)
			}
		},
	}
}
