// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build mage
// +build mage

package main

import (
	"fmt"
	"path/filepath"

	"github.com/magefile/mage/sh"

	// mage:import
	"github.com/elastic/elastic-agent/dev-tools/mage/target/common"

	// mage:import
	"github.com/elastic/elastic-agent-libs/dev-tools/mage"

	"github.com/elastic/elastic-agent-libs/dev-tools/mage/gotool"
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
	// Runs "go mod download all" which may update go.sum unnecessarily.
	err := mage.GenerateNotice(
		filepath.Join("dev-tools", "templates", "notice", "overrides.json"),
		filepath.Join("dev-tools", "templates", "notice", "rules.json"),
		filepath.Join("dev-tools", "templates", "notice", "NOTICE.txt.tmpl"),
	)
	if err != nil {
		return err
	}

	// Run go mod tidy to remove any changes to go.sum that aren't actually needed.
	// "go mod download all" isn't smart enough to know the minimum set of deps needed.
	// See https://github.com/golang/go/issues/43994#issuecomment-770053099
	return gotool.Mod.Tidy()
}
