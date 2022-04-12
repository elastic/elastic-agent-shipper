// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build mage
// +build mage

package main

import (
	"fmt"
	"github.com/magefile/mage/sh"
	"io"
	"os/exec"
	"sync"

	// Would like a named import here but it won't work with the lint aliases:
	// https://github.com/magefile/mage/issues/217
	// mage:import
	"github.com/elastic/elastic-agent-libs/dev-tools/mage"
	agenttools "github.com/elastic/elastic-agent/dev-tools/mage"

	// mage:import
	_ "github.com/elastic/elastic-agent/dev-tools/mage/target/common"
)

func init() {
	agenttools.BeatLicense = "Elastic License"
	agenttools.BeatDescription = "Shipper processes, queues, and ships data."
}

// Aliases are shortcuts to long target names.
// nolint:deadcode // it's used by `mage`.
var Aliases = map[string]interface{}{
	"llc":  mage.Linter.LastChange,
	"lint": mage.Linter.All,
}

func GenProto() error {
	return sh.Run("protoc", "-Iapi", "-Iapi/vendor", "--go_out=./api", "--go-grpc_out=./api", "--go_opt=paths=source_relative", "--go-grpc_opt=paths=source_relative", "api/shipper.proto")
}

func Build() error {
	return sh.Run("go", "build")
}

// Notice regenerates the NOTICE.txt file.
func Notice() error {
	fmt.Println(">> Generating NOTICE")
	fmt.Println(">> fmt - go mod tidy")
	err := sh.RunV("go", "mod", "tidy", "-v")
	if err != nil {
		return fmt.Errorf("running go mod tidy: %w", err)
	}
	fmt.Println(">> fmt - go mod download")
	err = sh.RunV("go", "mod", "download")
	if err != nil {
		return fmt.Errorf("running go mod download: %w", err)
	}
	fmt.Println(">> fmt - go list")
	str, err := sh.Output("go", "list", "-m", "-json", "all")
	if err != nil {
		return fmt.Errorf("running go list: %w", err)
	}
	fmt.Println(">> fmt - go run")
	cmd := exec.Command("go", "run", "go.elastic.co/go-licence-detector", "-includeIndirect", "-rules", "dev-tools/notice/rules.json", "-overrides", "dev-tools/notice/overrides.json", "-noticeTemplate", "dev-tools/notice/NOTICE.txt.tmpl",
		"-noticeOut", "NOTICE.txt", "-depsOut", "")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating go-licence-detector pipe: %w", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer stdin.Close()
		defer wg.Done()
		if _, err := io.WriteString(stdin, str); err != nil {
			fmt.Println(err)
		}
	}()
	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(stdoutStderr))
		return fmt.Errorf("running go-license-detector: %w", err)
	}
	wg.Wait()
	return nil
}
