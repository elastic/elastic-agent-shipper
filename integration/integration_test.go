// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.
//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/elastic/elastic-agent-shipper-client/pkg/proto"

	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent-shipper-client/pkg/helpers"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
)

type TestingEnvironment struct {
	t          *testing.T
	ctx        context.Context
	process    *os.Process
	stderrFile *os.File
	stdoutFile *os.File
	config     string
}

func NewTestingEnvironment(t *testing.T, config string) *TestingEnvironment {
	binaryFilename := os.Getenv("INTEGRATION_TEST_BINARY")
	_, err := os.Stat(binaryFilename)
	require.NoErrorf(t, err, "error reading binary %s: %s", binaryFilename, err)

	tempDir := t.TempDir()
	configFilename := filepath.Join(tempDir, "config.yml")
	configFile, err := os.Create(configFilename)
	require.NoErrorf(t, err, "error creating config file: %s: %s", configFilename, err)

	_, err = configFile.Write([]byte(config))
	require.NoErrorf(t, err, "error writing config file: %s: %s", configFilename, err)
	configFile.Close()

	stderrFilename := filepath.Join(tempDir, "stderr")
	stderrFile, err := os.Create(stderrFilename)
	require.NoErrorf(t, err, "error creating stderr file: %s: %s", stderrFilename, err)

	stdoutFilename := filepath.Join(tempDir, "stdout")
	stdoutFile, err := os.Create(stdoutFilename)
	require.NoErrorf(t, err, "error creating stdout file: %s: %w", stdoutFilename, err)

	args := []string{binaryFilename, "run", "-d", "*", "-e", "-c", configFilename}
	var procAttr os.ProcAttr
	procAttr.Files = []*os.File{os.Stdin, stdoutFile, stderrFile}
	process, err := os.StartProcess(binaryFilename, args, &procAttr)
	require.NoErrorf(t, err, "error creating process %s: %w", binaryFilename, err)

	deadline, ok := t.Deadline()
	if !ok {
		// default deadline if test is run without a deadline -timeout 0
		deadline = time.Now().Add(time.Second * 10)
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline.Add(time.Millisecond*-500))
	t.Cleanup(cancel)

	env := &TestingEnvironment{
		t:          t,
		ctx:        ctx,
		process:    process,
		stderrFile: stderrFile,
		stdoutFile: stdoutFile,
		config:     config,
	}

	return env
}

func (e *TestingEnvironment) Stop() {
	_ = e.process.Kill()
	_ = e.process.Release()
	err := e.stderrFile.Close()
	require.NoErrorf(e.t, err, "error closing stderr file: %s", err)
	err = e.stdoutFile.Close()
	require.NoErrorf(e.t, err, "error closing stdout file: %s", err)
}

func (e *TestingEnvironment) GetStderr() string {
	filename := e.stderrFile.Name()
	e.stderrFile.Sync()
	b, err := os.ReadFile(filename)
	require.NoErrorf(e.t, err, "error reading stderr: %s", err)
	return string(b)
}

func (e *TestingEnvironment) GetStdout() string {
	filename := e.stdoutFile.Name()
	e.stdoutFile.Sync()
	b, err := os.ReadFile(filename)
	require.NoErrorf(e.t, err, "error reading stdout: %s", err)
	return string(b)
}

// WaitUntil checks every second for the match string to show up in the
// location.  The location can be one of stderr or stdout
func (e *TestingEnvironment) WaitUntil(location string, match string) bool {
	var pFile *os.File
	var filename string
	ticker := time.NewTicker(time.Second)

	defer func() { ticker.Stop() }()

	switch location {
	case "stderr":
		pFile = e.stderrFile
		filename = e.stderrFile.Name()
	case "stdout":
		pFile = e.stdoutFile
		filename = e.stdoutFile.Name()
	default:
		e.t.Fatalf("Not a valid location: %s", location)
	}

	for {
		select {
		case <-ticker.C:
			pFile.Sync()
			b, err := os.ReadFile(filename)
			require.NoErrorf(e.t, err, "Error reading %s", filename)
			if strings.Contains(string(b), match) {
				return true
			}
		case <-e.ctx.Done():
			return false
		}
	}
}

// Contains checks for the match string in the location.  The location
// can be one of stderr or stdout
func (e *TestingEnvironment) Contains(location string, match string) bool {
	var pFile *os.File
	var filename string

	switch location {
	case "stderr":
		pFile = e.stderrFile
		filename = e.stderrFile.Name()
	case "stdout":
		pFile = e.stdoutFile
		filename = e.stdoutFile.Name()
	default:
		e.t.Fatalf("Not a valid location: %s", location)
	}

	pFile.Sync()
	b, err := os.ReadFile(filename)
	require.NoErrorf(e.t, err, "Error reading %s", filename)
	return strings.Contains(string(b), match)
}

func (e *TestingEnvironment) NewClient(addr string) pb.ProducerClient {
	opts := getDialOptions()
	conn, err := grpc.Dial(addr, opts...)
	require.NoErrorf(e.t, err, "Error Dialing %s: %s", addr, err)
	e.t.Cleanup(func() { conn.Close() })
	client := pb.NewProducerClient(conn)
	return client
}

// Fatalf is equivalent to t.Logf of error messaage, followed by
// t.Logf of stderr, followed by t.Logf of stdout and t.FailNow
func (e *TestingEnvironment) Fatalf(format string, args ...interface{}) {
	e.t.Logf(format, args...)

	e.t.Logf("config:\n%s\n", e.config)

	stderr := e.GetStderr()
	e.t.Logf("stderr:\n%s\n", stderr)

	stdout := e.GetStdout()
	e.t.Logf("stdout:\n%s\n", stdout)

	e.t.FailNow()
}

func createEvents(values []string) ([]*messages.Event, error) {
	events := make([]*messages.Event, len(values))

	for i, v := range values {
		fields, err := helpers.NewStruct(map[string]interface{}{
			"string": v,
			"number": 42,
		})
		if err != nil {
			return nil, fmt.Errorf("error making fields: %w", err)
		}
		e := &messages.Event{
			Timestamp: timestamppb.Now(),
			Source: &messages.Source{
				InputId:  "input",
				StreamId: "stream",
			},
			DataStream: &messages.DataStream{
				Type:      "log",
				Dataset:   "default",
				Namespace: "default",
			},
			Metadata: fields,
			Fields:   fields,
		}
		events[i] = e
	}
	return events, nil
}
