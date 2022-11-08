// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package cmd

import (
	"fmt"
	"os"

	"github.com/elastic/elastic-agent-shipper/tools"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Version returns version and build information.",
		Long:  `Version returns version and build information.`,
		Run: func(c *cobra.Command, args []string) {
			fmt.Fprintf(os.Stdout, "elastic-agent-shipper\tversion: %s\n", tools.GetDefaultVersion())
			fmt.Fprintf(os.Stdout, "\tbuild_commit: %s\tbuild_time: %s\tsnapshot_build: %v\n", tools.Commit(), tools.BuildTime(), tools.Snapshot())
		},
	}

	return cmd
}
