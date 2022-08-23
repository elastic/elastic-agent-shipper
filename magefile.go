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
	"github.com/pkg/errors"

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

// All builds binaries for the all os/arch.
func (Build) All() {
	os.Setenv("PLATFORM", "all")
	mg.Deps(Build.Binary)
}

// Clean removes the build directory.
func (Build) Clean(test string) {
	os.RemoveAll("build") // nolint:errcheck //not required
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
	env := map[string]string{
		"CGO_ENABLED":     devtools.EnvOrDefault("CGO_ENABLED", "0"),
		"DEV":             devtools.EnvOrDefault("DEV", "false"),
		"DEFAULT_VERSION": tools.DefaultBeatVersion,
	}

	versionQualifier, versionQualified := os.LookupEnv("VERSION_QUALIFIER")
	if versionQualified {
		env["VERSION_QUALIFIER"] = versionQualifier
		env["DEFAULT_VERSION"] += fmt.Sprintf("-%s", versionQualifier)
	}

	if snapshotEnv := os.Getenv("SNAPSHOT"); snapshotEnv != "" {
		isSnapshot, err := strconv.ParseBool(snapshotEnv)
		if err != nil {
			return err
		}
		if isSnapshot {
			args = append(args, "--snapshot")
			env["DEFAULT_VERSION"] += fmt.Sprintf("-%s", "SNAPSHOT")
		}
	}

	platform := os.Getenv("PLATFORM")
	if platform != "" && devtools.PlatformFiles[platform] == nil {
		return errors.New("Platform not recognized, only supported options: all, darwin, linux, windows, darwin/amd64, darwin/arm64, linux/386, linux/amd64, linux/arm64, windows/386, windows/amd64")
	}
	switch platform {
	case "windows", "linux", "darwin":
		args = append(args, "--id", platform)
	case "darwin/amd64", "darwin/arm64", "linux/386", "linux/amd64", "linux/arm64", "windows/386", "windows/amd64":
		goos := strings.Split(platform, "/")[0]
		arch := strings.Split(platform, "/")[1]
		env["GOOS"] = goos
		env["GOARCH"] = arch
		args = append(args, "--id", goos, "--single-target")
	case "all":
	default:
		goos := runtime.GOOS
		goarch := runtime.GOARCH
		platform = goos + "/" + goarch
		args = append(args, "--id", goos, "--single-target")
	}
	fmt.Println(">> build: Building binary for", platform) //nolint:forbidigo // it's ok to use fmt.println in mage
	err := sh.RunWithV(env, "goreleaser", args...)
	if err != nil {
		return errors.Wrapf(err, "Build failed on %s", platform)
	}
	return CheckBinaries(platform, env["DEFAULT_VERSION"])

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
	testCoverage := devtools.BoolEnvOrFalse("TEST_COVERAGE")
	//mg.Deps(Build.Binary, Build.TestBinaries)
	return devtools.GoUnitTest(ctx, testCoverage)
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

// CheckBinaries checks if the binaries are generated (for now).
func CheckBinaries(platform string, version string) error {
	path := filepath.Join("build", "binaries")
	selectedPlatformFiles := devtools.PlatformFiles[platform]
	if selectedPlatformFiles == nil {
		return errors.New("No selected platform files found")
	}
	for _, platform := range selectedPlatformFiles {
		var execName = devtools.ProjectName
		if strings.Contains(platform, "windows") {
			execName += ".exe"
		}
		binary := filepath.Join(path, fmt.Sprintf("%s-%s-%s", devtools.ProjectName, version, platform), execName)
		if _, err := os.Stat(binary); err != nil {
			return errors.Wrap(err, "Build: binary check failed")
		}
	}

	return nil
}

// License generates the license headers or returns an error
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
