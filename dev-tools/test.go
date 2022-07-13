// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package dev_tools

import (
	"context"
	"fmt"
	"github.com/magefile/mage/mg"
	"github.com/pkg/errors"
	"io"
	"os"
	"os/exec"
)

func GoUnitTest(ctx context.Context) error {
	mg.Deps(InstallGoTestTools)

	fmt.Println(">> go test:", "Unit Testing")

	gotestsumArgs := []string{"--no-color"}
	if mg.Verbose() {
		gotestsumArgs = append(gotestsumArgs, "-f", "standard-verbose")
	} else {
		gotestsumArgs = append(gotestsumArgs, "-f", "standard-quiet")
	}
	//create report files
	fileName := "build/TEST-go-unit"
	CreateDir(fileName + ".xml")
	gotestsumArgs = append(gotestsumArgs, "--junitfile", fileName+".xml")
	CreateDir(fileName + ".out")
	gotestsumArgs = append(gotestsumArgs, "--jsonfile", fileName+".out"+".json")

	var testArgs []string

	testArgs = append(testArgs, []string{"./..."}...)

	args := append(gotestsumArgs, append([]string{"--"}, testArgs...)...)

	goTest := MakeCommand(ctx, map[string]string{}, "gotestsum", args...)
	// Wire up the outputs.
	var outputs []io.Writer
	fileOutput, err := os.Create(CreateDir(fileName + ".out"))
	if err != nil {
		return errors.Wrap(err, "failed to create go test output file")
	}
	defer fileOutput.Close()
	outputs = append(outputs, fileOutput)

	output := io.MultiWriter(outputs...)
	goTest.Stdout = io.MultiWriter(output, os.Stdout)
	goTest.Stderr = io.MultiWriter(output, os.Stderr)
	err = goTest.Run()

	var goTestErr *exec.ExitError
	if err != nil {
		// Command ran.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return errors.Wrap(err, "failed to execute go")
		}
		// Command ran but failed. Process the output.
		goTestErr = exitErr
	}

	// Return an error indicating that testing failed.
	if goTestErr != nil {
		fmt.Println(">> go test:", "Unit Tests : Test Failed")
		return errors.Wrap(goTestErr, "go test returned a non-zero value")
	}

	fmt.Println(">> go test:", "Unit Tests : Test Passed")
	return nil
}
