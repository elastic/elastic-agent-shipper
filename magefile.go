// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build mage
// +build mage

package main

import (
	"path/filepath"

	"github.com/magefile/mage/sh"

	// mage:import
	_ "github.com/elastic/elastic-agent/dev-tools/mage/target/common"
	// mage:import
	"github.com/elastic/elastic-agent-libs/dev-tools/mage"

	devtools "github.com/elastic/elastic-agent/dev-tools/mage"
)

func init() {
	devtools.BeatLicense = "Elastic License"
	devtools.BeatDescription = "Shipper processes, queues, and ships data."
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

// Notice generates a NOTICE.txt file for the module.
func Notice() error {
	return mage.GenerateNotice(
		filepath.Join("dev-tools", "templates", "notice", "overrides.json"),
		filepath.Join("dev-tools", "templates", "notice", "rules.json"),
		filepath.Join("dev-tools", "templates", "notice", "NOTICE.txt.tmpl"),
	)

}
