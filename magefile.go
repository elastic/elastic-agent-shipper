// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build mage
// +build mage

package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"path"
	"path/filepath"

	"github.com/magefile/mage/sh"

	// mage:import
	"github.com/elastic/elastic-agent/dev-tools/mage/target/common"
	// mage:import
	"github.com/elastic/elastic-agent-libs/dev-tools/mage"
	"github.com/elastic/elastic-agent-libs/dev-tools/mage/gotool"
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

// Add here new packages that have to be compiled.
// Vendor packages are not included since they already have compiled versions.
// All `.proto` files in the listed directories will be compiled to Go.
var protoPackages = []string{
	"api",
	"api/messages",
}

func GenProto() error {
	var toCompile []string

	for _, p := range protoPackages {
		log.Printf("Listing the %s package...\n", p)

		files, err := ioutil.ReadDir(p)
		if err != nil {
			return fmt.Errorf("failed to read the proto package directory %s: %w", p, err)
		}
		for _, f := range files {
			if path.Ext(f.Name()) != ".proto" {
				continue
			}
			toCompile = append(toCompile, path.Join(p, f.Name()))
		}
	}

	args := append(
		[]string{"-Iapi", "-Iapi/vendor", "--go_out=./api", "--go-grpc_out=./api", "--go_opt=paths=source_relative", "--go-grpc_opt=paths=source_relative"},
		toCompile...,
	)

	log.Printf("Compiling %d packages...\n", len(protoPackages))
	err := sh.Run("protoc", args...)
	if err != nil {
		return fmt.Errorf("failed to compile protobuf: %w", err)
	}

	log.Println("Formatting generated code...")
	common.Fmt()
	return nil
}

func Build() {
	sh.Run("go", "build")
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
