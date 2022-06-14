// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build mage
// +build mage

package main

import (
	"fmt"
	"github.com/magefile/mage/sh"
	"path/filepath"

	// mage:import
	"github.com/elastic/elastic-agent/dev-tools/mage/target/common"

	// mage:import
	"github.com/elastic/elastic-agent-libs/dev-tools/mage"

	//beatsdev "github.com/elastic/beats/v7/dev-tools/mage"
	devtools "github.com/elastic/elastic-agent/dev-tools/mage"
)

const packageSpecFile = "dev-tools/packaging/packages.yml"

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

func Package() {
	devtools.Build(devtools.DefaultGolangCrossBuildArgs())
	// This seems like it shouldn't be needed, but
	// the build tooling for beats wants this,
	// And for now agent is treating it like a beat
	devtools.SetBuildVariableSources(
		&devtools.BuildVariableSources{
			BeatVersion: "./version/version.go",
			GoVersion:   ".go-version",
			DocBranch:   "./docs/version.asciidoc",
		})
	devtools.MustUsePackaging("elastic_shipper_agent", packageSpecFile)
	err := devtools.Package()
	if err != nil {
		fmt.Printf("Error running package: %s", err)
	}
}

func GenProto() {
	sh.Run("protoc", "-Iapi", "-Iapi/vendor", "--go_out=./api", "--go-grpc_out=./api", "--go_opt=paths=source_relative", "--go-grpc_opt=paths=source_relative", "api/shipper.proto")
	common.Fmt()
}

func Build() {
	//sh.Run("go", "build")
	devtools.Build(devtools.DefaultGolangCrossBuildArgs())
}

// Notice generates a NOTICE.txt file for the module.
func Notice() error {
	return mage.GenerateNotice(
		filepath.Join("dev-tools", "templates", "notice", "overrides.json"),
		filepath.Join("dev-tools", "templates", "notice", "rules.json"),
		filepath.Join("dev-tools", "templates", "notice", "NOTICE.txt.tmpl"),
	)
}
