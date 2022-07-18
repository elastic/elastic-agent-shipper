// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package main

import (
	"fmt"
	"os"

	"github.com/elastic/elastic-agent-shipper/cmd"
)

var (
	// BuildTime is the build time of the binary (set externally with ldflags).
	BuildTime = "unknown"

	// CommitHash is the Git hash of the branch, used for version purposes (set externally with ldflags).
	CommitHash = "undefined"

	// Tag describes the semver version of the application (set externally with ldflags).
	Tag string
)

func main() {
	command := cmd.NewCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

}
