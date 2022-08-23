// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package common

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"io"
	"os"
	"os/exec"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
	"github.com/pkg/errors"
)

func GoUnitTest(ctx context.Context, testCoverage bool) error {
	mg.Deps(InstallGoTestTools)

	fmt.Println(">> go test:", "Unit Testing") //nolint:forbidigo // just for tests
	var testArgs []string

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

	covFile := fileName + ".cov"
	if testCoverage {
		CreateDir(covFile)
		covFile = CreateDir(filepath.Clean(covFile))
		testArgs = append(testArgs,
			"-covermode=atomic",
			"-coverprofile="+covFile,
		)
	}

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

	// Generate a HTML code coverage report.
	var htmlCoverReport string
	if testCoverage {
		htmlCoverReport = strings.TrimSuffix(covFile,
			filepath.Ext(covFile)) + ".html"
		coverToHTML := sh.RunCmd("go", "tool", "cover",
			"-html="+covFile,
			"-o", htmlCoverReport)
		if err = coverToHTML(); err != nil {
			return errors.Wrap(err, "failed to write HTML code coverage report")
		}
	}

	// Generate an XML code coverage report.
	var codecovReport string
	if testCoverage {
		fmt.Println(">> go run gocover-cobertura:", covFile, "Started") //nolint:forbidigo // just for tests

		// execute gocover-cobertura in order to create cobertura report
		// install pre-requisites
		installCobertura := sh.RunCmd("go", "install", "github.com/boumenot/gocover-cobertura@latest")
		if err = installCobertura(); err != nil {
			return errors.Wrap(err, "failed to install gocover-cobertura")
		}

		codecovReport = strings.TrimSuffix(covFile,
			filepath.Ext(covFile)) + "-cov.xml"

		coverage, err := ioutil.ReadFile(covFile)
		if err != nil {
			return errors.Wrap(err, "failed to read code coverage report")
		}

		coberturaFile, err := os.Create(codecovReport)
		if err != nil {
			return err
		}
		defer coberturaFile.Close()

		coverToXML := exec.Command("gocover-cobertura")
		coverToXML.Stdout = coberturaFile
		coverToXML.Stderr = os.Stderr
		coverToXML.Stdin = bytes.NewReader(coverage)
		if err = coverToXML.Run(); err != nil {
			return errors.Wrap(err, "failed to write XML code coverage report")
		}
		fmt.Println(">> go run gocover-cobertura:", covFile, "Created") //nolint:forbidigo // just for tests
	}
	// Return an error indicating that testing failed.
	if goTestErr != nil {
		fmt.Println(">> go test:", "Unit Tests : Test Failed") //nolint:forbidigo // just for tests
		return errors.Wrap(goTestErr, "go test returned a non-zero value")
	}

	fmt.Println(">> go test:", "Unit Tests : Test Passed") //nolint:forbidigo // just for tests
	return nil
}
