// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build mage
// +build mage

package main

import (
	"github.com/magefile/mage/sh"

	// Would like a named import here but it won't work with the lint aliases:
	// https://github.com/magefile/mage/issues/217
	// mage:import
	"github.com/elastic/elastic-agent-libs/dev-tools/mage"
	agenttools "github.com/elastic/elastic-agent/dev-tools/mage"

	// mage:import
	_ "github.com/elastic/elastic-agent/dev-tools/mage/target/common"
)

func init() {
	agenttools.BeatLicense = "Elastic License"
	agenttools.BeatDescription = "Shipper processes, queues, and ships data."
}

// Aliases are shortcuts to long target names.
// nolint: deadcode // it's used by `mage`.
var Aliases = map[string]interface{}{
	"llc":  mage.Linter.LastChange,
	"lint": mage.Linter.All,
}

func GenProto() {
	sh.Run("protoc", "-Iapi", "-Iapi/vendor", "--go_out=./api", "--go-grpc_out=./api", "--go_opt=paths=source_relative", "--go-grpc_opt=paths=source_relative", "api/shipper.proto")
}

func Build() {
	sh.Run("go", "build")
}
