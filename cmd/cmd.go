// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

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
	"github.com/elastic/elastic-agent-shipper/server"
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
