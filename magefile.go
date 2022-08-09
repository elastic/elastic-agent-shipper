// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build mage
// +build mage

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/elastic/elastic-agent-libs/dev-tools/mage"
	devtools "github.com/elastic/elastic-agent-shipper/dev-tools/common"
	"github.com/elastic/elastic-agent-shipper/tools"

	//mage:import

	"github.com/elastic/elastic-agent-libs/dev-tools/mage/gotool"
	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const (
	GoreleaserRepo = "github.com/goreleaser/goreleaser@v1.6.3"
)

// Aliases are shortcuts to long target names.
// nolint: deadcode // it's used by `mage`.
var Aliases = map[string]interface{}{
	"build":           Build.Binary,
	"unitTest":        Test.Unit,
	"integrationTest": Test.Integration,
}

// BUILD

// Build contains targets related to linting the Go code
type Build mg.Namespace

// All builds binaries for the all  os/arch.
func (Build) All() {
	os.Setenv("PLATFORM", "all")
	mg.Deps(Build.Binary)
}

// Clean removes the build directory.
func (Build) Clean(test string) {
	os.RemoveAll("build") // nolint:errcheck //not required
}

// CheckBinaries checks if the binaries are generated (for now).
func (Build) CheckBinaries() error {
	path := filepath.Join("build", "binaries")
	for _, platform := range devtools.PlatformFiles {
		var execName = "elastic-agent-shipper"
		if strings.Contains(platform, "windows") {
			execName += ".exe"
		}
		binary := filepath.Join(path, fmt.Sprintf("%s-%s-%s", "elastic-agent-shipper", tools.DefaultBeatVersion, platform), execName)
		if _, err := os.Stat(binary); os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

// InstallGoReleaser target installs goreleaser
func InstallGoReleaser() error {
	return gotool.Install(
		gotool.Install.Package(GoreleaserRepo),
	)
}

// Binary will create the project binaries found in /build/binaries (use `mage build`)
// ENV PLATFORM = all, darwin, linux, windows, darwin/amd64, darwin/arm64, linux/386, linux/amd64, linux/arm64, windows/386, windows/amd64
// ENV SNAPSHOT = true/false
// ENV DEV = true/false
func (Build) Binary() error {
	InstallGoReleaser()

	args := []string{"build", "--rm-dist", "--skip-validate"}

	// Environment variable
	version := tools.DefaultBeatVersion
	env := map[string]string{
		"CGO_ENABLED": devtools.EnvOrDefault("CGO_ENABLED", "0"),
		"DEV":         devtools.EnvOrDefault("DEV", "false"),
	}

	if snapshotEnv := os.Getenv("SNAPSHOT"); snapshotEnv != "" {
		isSnapshot, err := strconv.ParseBool(snapshotEnv)
		if err != nil {
			return err
		}
		if isSnapshot {
			args = append(args, "--snapshot")
			version += "-SNAPSHOT"
		}
	}
	env["DEFAULT_VERSION"] = version

	platforms := os.Getenv("PLATFORM")
	switch platforms {
	case "windows", "linux", "darwin":
		args = append(args, "--id", platforms)
	case "darwin/amd64", "darwin/arm64", "linux/386", "linux/amd64", "linux/arm64", "windows/386", "windows/amd64":
		goos := strings.Split(platforms, "/")[0]
		arch := strings.Split(platforms, "/")[1]
		env["GOOS"] = goos
		env["GOARCH"] = arch
		args = append(args, "--id", goos, "--single-target")
	case "all":
	default:
		goos := runtime.GOOS
		args = append(args, "--id", goos, "--single-target")
	}
	fmt.Println(">> build: Building binary for", platforms) //nolint:forbidigo // it's ok to use fmt.println in mage
	sh.RunWithV(env, "goreleaser", args...)
	return nil
}

// TEST

// Test namespace contains all the task for testing the projects.
type Test mg.Namespace

// All runs all the tests.
func (Test) All() {
	mg.SerialDeps(Test.Unit, Test.Integration)
}

// Integration runs all the integration tests (use alias `mage integrationTest`).
func (Test) Integration(ctx context.Context) error {
	return nil
}

// Unit runs all the unit tests (use alias `mage unitTest`).
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
	mg.Deps(mage.InstallGoLicenser)

	return gotool.Licenser(
		gotool.Licenser.License("Elastic"),
		gotool.Licenser.Check(),
	)
}

// License should generate the license headers
func License() error {
	mg.Deps(mage.InstallGoLicenser)
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
