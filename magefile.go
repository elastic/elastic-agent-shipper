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
	"crypto/sha512"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/elastic/elastic-agent-libs/dev-tools/mage"
	"github.com/elastic/elastic-agent-libs/dev-tools/mage/gotool"
	devtools "github.com/elastic/elastic-agent-shipper/dev-tools/common"
	"github.com/elastic/elastic-agent-shipper/tools"
	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const (
	GoreleaserRepo   = "github.com/goreleaser/goreleaser@v1.6.3"
	platformsEnvName = "PLATFORMS"
	specFileName     = devtools.ProjectName + ".spec.yml"
	configFileName   = devtools.ProjectName + ".yml"
)

// Aliases are shortcuts to long target names.
// nolint: deadcode // it's used by `mage`.
var Aliases = map[string]interface{}{
	"build":                                 Build.Binary,
	"package":                               Package.Artifacts,
	"unitTest":                              Test.Unit,
	"integrationTest":                       Test.Integration,
	"release-manager-dependencies":          Dependencies.Generate,
	"release-manager-dependencies-snapshot": Dependencies.Snapshot,
	"release-manager-dependencies-release":  Dependencies.Release,
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
		return fmt.Errorf("Platform %s not recognized, only supported options: all, darwin, linux, windows, darwin/amd64, darwin/arm64, linux/386, linux/amd64, linux/arm64, windows/386, windows/amd64", platform)
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
		return fmt.Errorf("Build failed on %s: %w", platform, err)
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
	platform := runtime.GOOS + "/" + runtime.GOARCH
	version := tools.DefaultBeatVersion
	os.Setenv("PLATFORMS", platform)
	mg.Deps(Build.Binary)
	binary, err := absoluteBinaryPath(version, platform)
	if err != nil {
		return fmt.Errorf("error determining native binary: %w", err)
	}
	os.Setenv("INTEGRATION_TEST_BINARY", binary)
	return devtools.GoIntegrationTest(ctx)
}

// Unit runs all the unit tests (use alias `mage unitTest`).
func (Test) Unit(ctx context.Context) error {
	testCoverage := devtools.BoolEnvOrFalse("TEST_COVERAGE")
	//mg.Deps(Build.Binary, Build.TestBinaries)
	return devtools.GoUnitTest(ctx, testCoverage)
}

//CHECKS

// Lint runs the linter, equivalent running to 'golangci-lint run ./...'
func Lint() error {
	return mage.Linter{}.All()
}

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
			return fmt.Errorf("Build: binary check failed: %w", err)
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

// RELEASE HELPERS

// Version returns current stack version, used for the release process
func Version() {
	fmt.Println(tools.DefaultBeatVersion)
}

// DEPENDENCIES

// Dependencies contains targets related to generating dependencies csv file
type Dependencies mg.Namespace

// Generate creates a list of dependencies in a form of csv file.
func (Dependencies) Generate() {
	dependenciesDir := filepath.Join("build", "distributions", "reports")
	err := os.MkdirAll(dependenciesDir, 0755)
	if err != nil {
		panic(err)
	}

	runWithGoPath := filepath.Join("dev-tools", "run_with_go_ver")
	runWithGoPath, err = filepath.Abs(runWithGoPath)
	if err != nil {
		panic(err)
	}
	dependenciesReportPath := filepath.Join("dev-tools", "dependencies-report")

	version, err := fullVersion()
	if err != nil {
		panic(err)
	}
	csvPath := filepath.Join("build", "distributions", "reports", "dependencies-"+version+".csv")

	cmd := exec.Command(runWithGoPath, dependenciesReportPath, "--csv", csvPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		panic(err)
	}
}

// Snapshot prepares the dependencies file for a snapshot
func (Dependencies) Snapshot() {
	os.Setenv("SNAPSHOT", "true")
	mg.Deps(Dependencies.Generate)
}

// Release prepares the dependencies file for a release
func (Dependencies) Release() {
	mg.Deps(Dependencies.Generate)
}

// PACKAGE

// Package contains targets related to producting project archives
type Package mg.Namespace

// All builds binaries for the all os/arch and produces resulting archives.
func (Package) All() {
	os.Setenv("PLATFORMS", "all")
	mg.Deps(Package.Artifacts)
}

// Package runs all the checks including licence, notice, gomod, git changes, followed by binary build.
func (Package) Artifacts() {
	// these are not allowed in parallel
	mg.SerialDeps(
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

	archivePath := filepath.Join("build", "distributions")
	if err := os.MkdirAll(archivePath, 0755); err != nil {
		panic(err)
	}

	for _, platform := range platforms {
		isWindows := strings.Contains(platform, "windows")

		// grab binary
		binaryPath := binaryFilePath(version, platform)

		// prepare package
		var err error
		var archiveFileName string
		targetDir := fmt.Sprintf("%s-%s", devtools.ProjectName, platform)
		if isWindows {
			archiveFileName = fmt.Sprintf("%s-%s-%s.zip", devtools.ProjectName, version, platform)
			err = prepareZipArchive(
				archivePath,
				targetDir,
				archiveFileName,
				map[string]string{
					specFileName:       specFileName,
					configFileName:     configFileName,
					execName(platform): binaryPath,
				},
			)
		} else {
			archiveFileName = fmt.Sprintf("%s-%s-%s.tar.gz", devtools.ProjectName, version, platform)
			err = prepareTarArchive(
				archivePath,
				targetDir,
				archiveFileName,
				map[string]string{
					specFileName:       specFileName,
					configFileName:     configFileName,
					execName(platform): binaryPath,
				},
			)
		}

		if err != nil {
			panic(err)
		}

		// generate sha sum
		archiveFullPath := filepath.Join(archivePath, archiveFileName)
		archive, err := os.Open(archiveFullPath)
		if err != nil {
			panic(fmt.Errorf("failed opening archive %q: %w", archiveFileName, err))
		}

		h := sha512.New()
		if _, err := io.Copy(h, archive); err != nil {
			panic(fmt.Errorf("failed computing hash for archive %q: %w", archiveFileName, err))
		}

		shaFile := archiveFullPath + ".sha512"
		content := fmt.Sprintf("%x  %s\n", h.Sum(nil), archiveFileName)
		if err := ioutil.WriteFile(shaFile, []byte(content), 0644); err != nil {
			panic(fmt.Errorf("failed writing hash for archive %q: %w", archiveFileName, err))
		}

		archive.Close()
	}
}

func prepareZipArchive(path, targetDir, archiveFileName string, files map[string]string) error {
	archiveFullName := filepath.Join(path, archiveFileName)
	archiveFile, err := os.Create(archiveFullName)
	if err != nil {
		return fmt.Errorf("failed to create %q: %w", archiveFullName, err)
	}
	defer archiveFile.Close()

	zipWriter := zip.NewWriter(archiveFile)
	defer zipWriter.Close()

	addFile := func(fileName, filePath string, zipWriter *zip.Writer) error {
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed opening a file: %w", err)
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			return fmt.Errorf("failed retrieving stat info for a file: %w", err)
		}

		header, err := zip.FileInfoHeader(stat)
		header.Method = zip.Deflate
		header.Name = filepath.Join(targetDir, fileName)

		hWriter, err := zipWriter.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("failed writing zip header: %w", err)
		}

		if _, err := io.Copy(hWriter, file); err != nil {
			return fmt.Errorf("failed adding a file into an archive: %w", err)
		}
		return nil
	}

	for fileName, filePath := range files {
		if err := addFile(fileName, filePath, zipWriter); err != nil {
			return fmt.Errorf("failed adding file %q into zip archive: %w", filePath, err)
		}
	}

	return nil
}

func prepareTarArchive(path, targetDir, archiveFileName string, files map[string]string) error {
	archiveFullName := filepath.Join(path, archiveFileName)
	archiveFile, err := os.Create(archiveFullName)
	if err != nil {
		return fmt.Errorf("failed to create %q: %w", archiveFullName, err)
	}
	defer archiveFile.Close()

	gzipWriter := gzip.NewWriter(archiveFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	addFile := func(fileName, filePath string, tarWriter *tar.Writer) error {
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed opening a file: %w", err)
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			return fmt.Errorf("failed retrieving stat info for a file: %w", err)
		}

		header := &tar.Header{
			Name:    filepath.Join(targetDir, fileName),
			Size:    stat.Size(),
			Mode:    int64(stat.Mode()),
			ModTime: stat.ModTime(),
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed writing tar header: %w", err)
		}

		if _, err := io.Copy(tarWriter, file); err != nil {
			return fmt.Errorf("failed adding a file into an archive: %w", err)
		}

		return nil
	}

	for fileName, filePath := range files {
		if err := addFile(fileName, filePath, tarWriter); err != nil {
			return fmt.Errorf("failed adding file %q into tar archive: %w", filePath, err)
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
	return filepath.Join(path, fmt.Sprintf("%s-%s-%s", devtools.ProjectName, version, platform), execName(platform))
}

func execName(platform string) string {
	var execName = devtools.ProjectName
	if strings.Contains(platform, "windows") {
		execName += ".exe"
	}

	return execName
}

func absoluteBinaryPath(version, platform string) (string, error) {
	binary := ""
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("unable to get current working directory: %w", err)
	}
	selectedPlatformFiles := devtools.PlatformFiles[platform]
	if selectedPlatformFiles == nil {
		return "", fmt.Errorf("no platform files found for %s", platform)
	}

	for _, pf := range selectedPlatformFiles {
		binary = filepath.Join(dir, binaryFilePath(version, pf))
		if _, err := os.Stat(binary); err != nil {
			return "", fmt.Errorf("error reading %s: %w", binary, err)
		}
	}
	return binary, nil
}
