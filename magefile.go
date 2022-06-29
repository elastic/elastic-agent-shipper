// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build mage
// +build mage

package main

import (
	"path/filepath"

	"github.com/elastic/elastic-agent-libs/dev-tools/mage"
	devtools "github.com/elastic/elastic-agent-libs/dev-tools/mage"
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
	"llc":  mage.Linter.LastChange,
	"lint": mage.Linter.All,
}

func Build() {
	sh.Run("go", "build")
}

// Check runs all the checks
func Check() {
	mg.Deps(
		CheckLicense,
		devtools.Deps.CheckModuleTidy,
		Notice,
		devtools.CheckNoChanges,
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

// CheckLicense checks the license headers
func License() error {
	mg.Deps(InstallLicenser)

	return gotool.Licenser(
		gotool.Licenser.License("Elastic"),
	)
}

// Notice generates a NOTICE.txt file for the module.
func Notice() error {
	// Runs "go mod download all" which may update go.sum unnecessarily.
	err := devtools.GenerateNotice(
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
