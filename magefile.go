// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build mage
// +build mage

package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
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
	GoreleaserRepo   = "github.com/goreleaser/goreleaser@v1.6.3"
	platformsEnvName = "PLATFORMS"
	specFileName     = devtools.ProjectName + ".spec"
	configFileName   = devtools.ProjectName + ".yml"
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
	os.Setenv("PLATFORMS", "all")
	mg.Deps(Build.Binary)
}

// Clean removes the build directory.
func (Build) Clean() {
	os.RemoveAll("build") // nolint:errcheck //not required
}

// InstallGoReleaser target installs goreleaser
func InstallGoReleaser() error {
	return gotool.Install(
		gotool.Install.Package(GoreleaserRepo),
	)
}

// Binary will create the project binaries found in /build/binaries (use `mage build`)
// ENV PLATFORMS = all, darwin, linux, windows, darwin/amd64, darwin/arm64, linux/386, linux/amd64, linux/arm64, windows/386, windows/amd64
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

	platform := os.Getenv(platformsEnvName)
	if platform != "" && devtools.PlatformFiles[platform] == nil {
		return errors.Errorf("Platform %s not recognized, only supported options: all, darwin, linux, windows, darwin/amd64, darwin/arm64, linux/386, linux/amd64, linux/arm64, windows/386, windows/amd64", platform)
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
		binary := binaryName(path, version, platform)
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

// PACKAGE

// Package runs all the checks including licence, notice, gomod, git changes, followed by binary build.
func Package() {
	// these are not allowed in parallel
	mg.SerialDeps(
		Build.Clean,
		CheckLicense,
		Notice,
		mage.Deps.CheckModuleTidy,
		mage.CheckNoChanges,
		Build.Binary,
	)

	version, err := fullVersion()
	if err != nil {
		panic(err)
	}

	platforms := devtools.PlatformFiles[os.Getenv(platformsEnvName)]
	// current by default
	if platforms == nil {
		platforms = devtools.PlatformFiles[runtime.GOOS+"/"+runtime.GOARCH]
	}

	commonFiles := []string{
		specFileName,
		configFileName,
	}
	archivePath := filepath.Join("build", "packages")
	if err := os.MkdirAll(archivePath, 0755); err != nil {
		panic(err)
	}

	for _, platform := range platforms {
		isWindows := platform == "windows-x86" || platform == "windows-x86_64"

		// grab binary
		binaryPath := binaryFilePath(version, platform)

		// prepare package
		var err error
		if isWindows {
			archiveFileName := devtools.ProjectName + platform + ".zip"
			err = prepareZipArchive(archivePath, archiveFileName, append(commonFiles, binaryPath))
		} else {
			archiveFileName := devtools.ProjectName + platform + ".tar.gz"
			err = prepareTarArchive(archivePath, archiveFileName, append(commonFiles, binaryPath))
		}

		if err != nil {
			panic(err)
		}

	}
}

func prepareZipArchive(path, archiveFileName string, files []string) error {
	// remove if it exists
	_ = os.Remove(path)

	archiveFullName := filepath.Join(path, archiveFileName)
	archiveFile, err := os.Create(archiveFullName)
	if err != nil {
		return errors.Wrapf(err, "failed to create %q", archiveFullName)
	}
	defer archiveFile.Close()

	zipWriter := zip.NewWriter(archiveFile)
	defer zipWriter.Close()

	addFile := func(filePath string, zipWriter *zip.Writer) error {
		file, err := os.Open(filePath)
		if err != nil {
			return errors.Wrap(err, "failed opening a file")
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			return errors.Wrap(err, "failed retrieving stat info for a file")
		}

		header, err := zip.FileInfoHeader(stat)
		header.Method = zip.Deflate

		hWriter, err := zipWriter.CreateHeader(header)
		if err != nil {
			return errors.Wrap(err, "failed writing zip header")
		}

		if _, err := io.Copy(hWriter, file); err != nil {
			return errors.Wrap(err, "failed adding a file into an archive")
		}
		return nil
	}

	for _, filePath := range files {
		if err := addFile(filePath, zipWriter); err != nil {
			return errors.Wrapf(err, "failed adding file %q into zip archive", filePath)
		}
	}

	return nil
}

func prepareTarArchive(path, archiveFileName string, files []string) error {
	// remove if it exists
	_ = os.Remove(path)

	archiveFullName := filepath.Join(path, archiveFileName)
	archiveFile, err := os.Create(archiveFullName)
	if err != nil {
		return errors.Wrapf(err, "failed to create %q", archiveFullName)
	}
	defer archiveFile.Close()

	gzipWriter := gzip.NewWriter(archiveFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	addFile := func(filePath string, tarWriter *tar.Writer) error {
		file, err := os.Open(filePath)
		if err != nil {
			return errors.Wrap(err, "failed opening a file")
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			return errors.Wrap(err, "failed retrieving stat info for a file")
		}

		header := &tar.Header{
			Name:    filePath,
			Size:    stat.Size(),
			Mode:    int64(stat.Mode()),
			ModTime: stat.ModTime(),
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			return errors.Wrap(err, "failed writing tar header")
		}

		if _, err := io.Copy(tarWriter, file); err != nil {
			return errors.Wrap(err, "failed adding a file into an archive")
		}

		return nil
	}

	for _, filePath := range files {
		if err := addFile(filePath, tarWriter); err != nil {
			return errors.Wrapf(err, "failed adding file %q into tar archive", filePath)
		}
	}

	return nil
}

func fullVersion() (string, error) {
	version := tools.DefaultBeatVersion
	if versionQualifier, versionQualified := os.LookupEnv("VERSION_QUALIFIER"); versionQualified {
		version += fmt.Sprintf("-%s", versionQualifier)
	}

	if snapshotEnv := os.Getenv("SNAPSHOT"); snapshotEnv != "" {
		isSnapshot, err := strconv.ParseBool(snapshotEnv)
		if err != nil {
			return "", err
		}
		if isSnapshot {
			version += fmt.Sprintf("-%s", "SNAPSHOT")
		}
	}

	return version, nil
}

func binaryFilePath(version, platform string) string {
	path := filepath.Join("build", "binaries")
	return binaryName(path, version, platform)
}

func binaryName(path, version, platform string) string {
	var execName = devtools.ProjectName
	if strings.Contains(platform, "windows") {
		execName += ".exe"
	}

	return filepath.Join(path, fmt.Sprintf("%s-%s-%s", devtools.ProjectName, version, platform), execName)
}
