// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package common

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/pkg/errors"

	"github.com/elastic/elastic-agent-libs/dev-tools/mage/gotool"
)

var PlatformFiles = []string{"darwin-aarch64", "darwin-x86_64", "linux-arm64", "linux-x86_64", "linux-x86", "windows-x86_64", "windows-x86"}

// CreateDir creates the parent directory for the given file.
func CreateDir(file string) string {
	// Create the output directory.
	if dir := filepath.Dir(file); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			panic(errors.Wrapf(err, "failed to create parent dir for %v", file))
		}
	}
	return file
}

func InstallGoTestTools() {
	gotool.Install(gotool.Install.Package("gotest.tools/gotestsum")) //nolint:errcheck //not required
}

func MakeCommand(ctx context.Context, env map[string]string, cmd string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, cmd, args...)
	c.Env = os.Environ()
	for k, v := range env {
		c.Env = append(c.Env, k+"="+v)
	}
	c.Stdout = ioutil.Discard
	if mg.Verbose() {
		c.Stdout = os.Stdout
	}
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	fmt.Println("exec:", cmd, strings.Join(args, " ")) //nolint:forbidigo // just for mage
	return c
}

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
