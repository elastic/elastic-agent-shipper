// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package common

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/magefile/mage/mg"

	"github.com/elastic/elastic-agent-libs/dev-tools/mage/gotool"
)

const ProjectName = "elastic-agent-shipper"

var PlatformFiles = map[string][]string{
	"windows":       {"windows-x86_64", "windows-x86"},
	"linux":         {"linux-arm64", "linux-x86_64", "linux-x86"},
	"darwin":        {"darwin-aarch64", "darwin-x86_64"},
	"darwin/amd64":  {"darwin-x86_64"},
	"darwin/arm64":  {"darwin-aarch64"},
	"linux/386":     {"linux-x86"},
	"linux/amd64":   {"linux-x86_64"},
	"linux/arm64":   {"linux-arm64"},
	"windows/386":   {"windows-x86"},
	"windows/amd64": {"windows-x86_64"},
	"all":           {"darwin-aarch64", "darwin-x86_64", "linux-arm64", "linux-x86_64", "linux-x86", "windows-x86_64", "windows-x86"},
}

// CreateDir creates the parent directory for the given file.
func CreateDir(file string) string {
	// Create the output directory.
	if dir := filepath.Dir(file); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			panic(fmt.Errorf("failed to create parent dir for %v: %w", file, err))
		}
	}
	return file
}

// InstallGoTestTools installs any go packages needed for testing
func InstallGoTestTools() {
	gotool.Install(gotool.Install.Package("gotest.tools/gotestsum")) //nolint:errcheck //not required
}

// MakeCommand creates new CLI exec structure
func MakeCommand(ctx context.Context, env map[string]string, cmd string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, cmd, args...)
	c.Env = os.Environ()
	for k, v := range env {
		c.Env = append(c.Env, k+"="+v)
	}
	c.Stdout = io.Discard
	if mg.Verbose() {
		c.Stdout = os.Stdout
	}
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	fmt.Println("exec:", cmd, strings.Join(args, " ")) //nolint:forbidigo // just for mage
	return c
}

// EnvOrDefault wraps Getenv with logic to handle bool variables
func EnvOrDefault(buildEnv string, defaultValue string) string {
	if val := os.Getenv(buildEnv); val != "" {
		boolVal, err := strconv.ParseBool(val)
		if err != nil {
			return defaultValue
		}
		return strconv.FormatBool(boolVal)
	}
	return defaultValue
}

// BoolEnvOrFalse checks if a bool environment variable is set
func BoolEnvOrFalse(buildEnv string) bool {
	if val := os.Getenv(buildEnv); val != "" {
		boolVal, err := strconv.ParseBool(val)
		if err != nil {
			return false
		}
		return boolVal
	}
	return false
}
