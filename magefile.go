// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build mage
// +build mage

package main

import (
	"context"
	"fmt"
	"github.com/elastic/elastic-agent-libs/dev-tools/mage"
	devtools "github.com/elastic/elastic-agent-shipper/dev-tools"
	"os"
	"path/filepath"
	"runtime"
	//mage:import

	"github.com/elastic/elastic-agent-libs/dev-tools/mage/gotool"
	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

var (
	goLicenserRepo = "github.com/elastic/go-licenser@v0.4.1"
)

// Aliases are shortcuts to long target names.
// nolint: deadcode // it's used by `mage`.
var Aliases = map[string]interface{}{
	"llc":      mage.Linter.LastChange,
	"lint":     mage.Linter.All,
	"build":    Build.Binary,
	"unittest": Test.Unit,
}

// BUILD

// Build contains targets related to linting the Go code
type Build mg.Namespace

// All builds binaries for the all  os/arch.
func (Build) All() {
	mg.Deps(Build.Binary)
}

// Clean removes the build directory.
func (Build) Clean(test string) {
	os.RemoveAll("build")
}

// TestBinaries checks if the binaries are generated (for now).
func (Build) TestBinaries() error {
	p := filepath.Join("build", "binaries")
	execName := "exec"
	if runtime.GOOS == "windows" {
		execName += ".exe"
	}
	_ = p
	return nil
}

// Binary will create the project binaries found in /build/binaries
func (Build) Binary() {
	fmt.Println(">> build: Building binary")
	sh.Run("goreleaser", "build", "--rm-dist", "--skip-validate")
}

// TEST

// Test namespace contains all the task for testing the projects.
type Test mg.Namespace

// All runs all the tests.
func (Test) All() {
	mg.SerialDeps(Test.Unit, Test.Integration)
}

// Integration runs all the integration tests (use alias `mage integrationtest`).
func (Test) Integration(ctx context.Context) error {
	return nil
}

// Unit runs all the unit tests (use alias `mage unittest`).
func (Test) Unit(ctx context.Context) error {
	//mg.Deps(Build.Binary, Build.TestBinaries)
	return devtools.GoUnitTest(ctx)
}

//CHECKS

// Check runs all the checks including licence, notice, gomod, git changes
func Check() {
	// these are not allowed in parallel
	mg.SerialDeps(
		CheckLicense,
		Notice,
		mage.Deps.CheckModuleTidy,
		mage.CheckNoChanges,
	)
}

// CheckLicense checks the license headers
func CheckLicense() error {
	mg.Deps(InstallLicenser)

	return gotool.Licenser(
		gotool.Licenser.License("Elastic"),
		gotool.Licenser.Check(),
	)
}

// InstallLicenser installs the licenser in the current environment
func InstallLicenser() error {
	return gotool.Install(gotool.Install.Package(goLicenserRepo))
}

// License should generate the license headers
func License() error {
	mg.Deps(InstallLicenser)
	return gotool.Licenser(
		gotool.Licenser.License("Elastic"),
	)
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
