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

	pb "github.com/elastic/elastic-agent-shipper-client/pkg/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/elastic/elastic-agent-shipper-client/pkg/helpers"
	"github.com/elastic/elastic-agent-shipper-client/pkg/proto/messages"
)

type TestingEnvironment struct {
	t          *testing.T
	ctx        context.Context
	process    *os.Process
	stderrFile *os.File
	stdoutFile *os.File
}

func NewTestingEnvironment(t *testing.T, config string) (*TestingEnvironment, error) {
	binaryFilename := os.Getenv("INTEGRATION_TEST_BINARY")
	if _, err := os.Stat(binaryFilename); err != nil {
		return nil, fmt.Errorf("error reading binary %s: %w", binaryFilename, err)
	}

	tempDir := t.TempDir()
	configFilename := filepath.Join(tempDir, "config.yml")
	configFile, err := os.Create(configFilename)
	if err != nil {
		return nil, fmt.Errorf("error creating config file: %s: %w", configFilename, err)
	}
	if _, err = configFile.Write([]byte(config)); err != nil {
		return nil, fmt.Errorf("error writing config file: %s: %w", configFilename, err)
	}
	configFile.Close()

	stderrFilename := filepath.Join(tempDir, "stderr")
	stderrFile, err := os.Create(stderrFilename)
	if err != nil {
		return nil, fmt.Errorf("error creating stderr file: %s: %w", stderrFilename, err)
	}

	stdoutFilename := filepath.Join(tempDir, "stdout")
	stdoutFile, err := os.Create(stdoutFilename)
	if err != nil {
		return nil, fmt.Errorf("error creating stdout file: %s: %w", stdoutFilename, err)
	}

	args := []string{binaryFilename, "run", "-c", configFilename}
	var procAttr os.ProcAttr
	procAttr.Files = []*os.File{os.Stdin, stdoutFile, stderrFile}
	process, err := os.StartProcess(binaryFilename, args, &procAttr)
	if err != nil {
		return nil, fmt.Errorf("error creating process %s: %w", binaryFilename, err)
	}

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
	}

	return env, nil
}

func (e *TestingEnvironment) Stop() {
	e.process.Kill()
	e.process.Release()
}

func (e *TestingEnvironment) GetStderr() (string, error) {
	filename := e.stderrFile.Name()
	e.stderrFile.Sync()
	b, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("error reading stderr: %w", err)
	}
	return string(b), nil
}

func (e *TestingEnvironment) GetStdout() (string, error) {
	filename := e.stdoutFile.Name()
	e.stdoutFile.Sync()
	b, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("error reading stdout: %w", err)
	}
	return string(b), nil
}

// WaitUntil checks every second for the match string to show up in the
// location.  The location can be one of stderr or stdout
func (e *TestingEnvironment) WaitUntil(location string, match string) (bool, error) {
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
		return false, fmt.Errorf("Not a valid location: %s", location)
	}

	for {
		select {
		case <-ticker.C:
			pFile.Sync()
			b, err := os.ReadFile(filename)
			if err != nil {
				return false, fmt.Errorf("Error reading %s", filename)
			}
			if strings.Contains(string(b), match) {
				return true, nil
			}
		case <-e.ctx.Done():
			return false, nil
		}
	}
}

func (e *TestingEnvironment) NewClient(addr string) (pb.ProducerClient, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}
	e.t.Cleanup(func() { conn.Close() })
	client := pb.NewProducerClient(conn)
	return client, nil
}

func TestServerStarts(t *testing.T) {
	var config strings.Builder

	_, _ = config.WriteString("server:\n")
	_, _ = config.WriteString("  strict_mode: false\n")
	_, _ = config.WriteString("  port: 50052\n")
	_, _ = config.WriteString("  tls: false\n")
	_, _ = config.WriteString("logging:\n")
	_, _ = config.WriteString("  level: debug\n")
	_, _ = config.WriteString("  selectors: [\"*\"]\n")
	_, _ = config.WriteString("  to_stderr: true\n")

	env, err := NewTestingEnvironment(t, config.String())
	if err != nil {
		t.Fatalf("Error creating environment: %s", err)
	}
	t.Cleanup(func() { env.Stop() })

	found, err := env.WaitUntil("stderr", "gRPC server is ready and is listening on")
	if err != nil {
		t.Fatalf("Error waiting for server to start: %s", err)
	}

	if !found {
		stderr, err := env.GetStderr()
		if err != nil {
			t.Errorf("Error getting standard error: %s", err)
		}
		stdout, err := env.GetStdout()
		if err != nil {
			t.Errorf("Error getting standard out: %s", err)
		}
		t.Fatalf("Test executable failed to start.\nstderr:\n%s\nstdout:\n%s\n", stderr, stdout)
	}
}

func TestServerFailsToStart(t *testing.T) {
	var config strings.Builder

	_, _ = config.WriteString("server:\n")
	_, _ = config.WriteString("  strict_mode: false\n")
	_, _ = config.WriteString("  port: 50052\n")
	_, _ = config.WriteString("  tls: not_boolean\n")
	_, _ = config.WriteString("logging:\n")
	_, _ = config.WriteString("  level: debug\n")
	_, _ = config.WriteString("  selectors: [\"*\"]\n")
	_, _ = config.WriteString("  to_stderr: true\n")

	env, err := NewTestingEnvironment(t, config.String())
	if err != nil {
		t.Fatalf("Error creating environment: %s", err)
	}
	t.Cleanup(func() { env.Stop() })

	found, err := env.WaitUntil("stderr", "error unpacking shipper config")
	if err != nil {
		t.Fatalf("Error waiting for error message: %s", err)
	}

	if !found {
		stderr, err := env.GetStderr()
		if err != nil {
			t.Errorf("Error getting standard error: %s", err)
		}
		stdout, err := env.GetStdout()
		if err != nil {
			t.Errorf("Error getting standard out: %s", err)
		}
		t.Fatalf("Test executable failed to start.\nstderr:\n%s\nstdout:\n%s\n", stderr, stdout)
	}
}

func TestPublishMessage(t *testing.T) {
	var config strings.Builder

	_, _ = config.WriteString("server:\n")
	_, _ = config.WriteString("  strict_mode: false\n")
	_, _ = config.WriteString("  port: 50052\n")
	_, _ = config.WriteString("  tls: false\n")
	_, _ = config.WriteString("logging:\n")
	_, _ = config.WriteString("  level: debug\n")
	_, _ = config.WriteString("  selectors: [\"*\"]\n")
	_, _ = config.WriteString("  to_stderr: true\n")

	env, err := NewTestingEnvironment(t, config.String())
	if err != nil {
		t.Fatalf("Error creating environment: %s", err)
	}
	t.Cleanup(func() { env.Stop() })

	found, err := env.WaitUntil("stderr", "gRPC server is ready and is listening on")
	if err != nil {
		t.Fatalf("Error waiting for server to start: %s", err)
	}

	if !found {
		stderr, err := env.GetStderr()
		if err != nil {
			t.Errorf("Error getting standard error: %s", err)
		}
		stdout, err := env.GetStdout()
		if err != nil {
			t.Errorf("Error getting standard out: %s", err)
		}
		t.Fatalf("Test executable failed to start.\nstderr:\n%s\nstdout:\n%s\n", stderr, stdout)
	}

	client, err := env.NewClient("localhost:50052")
	if err != nil {
		t.Fatalf("Error creating client: %s", err)
	}

	unique := "UniqueStringToLookForInOutput"
	events, err := createEvents([]string{unique})
	if err != nil {
		t.Fatalf("error creating events: %s\n", err)
	}

	_, err = client.PublishEvents(env.ctx, &messages.PublishRequest{
		Events: events,
	})
	if err != nil {
		t.Fatalf("Error publishing event: %s", err)
	}

	found, err = env.WaitUntil("stdout", unique)
	if err != nil {
		t.Fatalf("Error looking for publish results: %s", err)
	}

	if !found {
		stderr, err := env.GetStderr()
		if err != nil {
			t.Errorf("Error getting standard error: %s", err)
		}
		stdout, err := env.GetStdout()
		if err != nil {
			t.Errorf("Error getting standard out: %s", err)
		}
		t.Fatalf("Event wasn't published.\nstderr:\n%s\nstdout:\n%s\n", stderr, stdout)
	}
}

func TestDiskQueue(t *testing.T) {
	var config strings.Builder
	queue_path := t.TempDir()

	_, _ = config.WriteString("server:\n")
	_, _ = config.WriteString("  strict_mode: false\n")
	_, _ = config.WriteString("  port: 50052\n")
	_, _ = config.WriteString("  tls: false\n")
	_, _ = config.WriteString("logging:\n")
	_, _ = config.WriteString("  level: debug\n")
	_, _ = config.WriteString("  selectors: [\"*\"]\n")
	_, _ = config.WriteString("  to_stderr: true\n")
	_, _ = config.WriteString("queue:\n")
	_, _ = config.WriteString("  disk:\n")
	_, _ = config.WriteString("    path: " + queue_path + "\n")
	_, _ = config.WriteString("    max_size: 10G\n")

	env, err := NewTestingEnvironment(t, config.String())
	if err != nil {
		t.Fatalf("Error creating environment: %s", err)
	}
	t.Cleanup(func() { env.Stop() })

	found, err := env.WaitUntil("stderr", "gRPC server is ready and is listening on")
	if err != nil {
		t.Fatalf("Error waiting for server to start: %s", err)
	}

	if !found {
		stderr, err := env.GetStderr()
		if err != nil {
			t.Errorf("Error getting standard error: %s", err)
		}
		stdout, err := env.GetStdout()
		if err != nil {
			t.Errorf("Error getting standard out: %s", err)
		}
		t.Fatalf("Test executable failed to start.\nstderr:\n%s\nstdout:\n%s\n", stderr, stdout)
	}

	client, err := env.NewClient("localhost:50052")
	if err != nil {
		t.Fatalf("Error creating client: %s", err)
	}

	unique := "UniqueStringToLookForInOutput"
	events, err := createEvents([]string{unique})
	if err != nil {
		t.Fatalf("error creating events: %s\n", err)
	}

	_, err = client.PublishEvents(env.ctx, &messages.PublishRequest{
		Events: events,
	})
	if err != nil {
		stderr, err := env.GetStderr()
		if err != nil {
			t.Errorf("Error getting standard error: %s", err)
		}
		stdout, err := env.GetStdout()
		if err != nil {
			t.Errorf("Error getting standard out: %s", err)
		}
		t.Fatalf("Error publishing event: %s\nstderr:\n%s\nstdout:\n%s\n", err, stderr, stdout)
	}

	found, err = env.WaitUntil("stdout", unique)
	if err != nil {
		t.Fatalf("Error looking for publish results: %s", err)
	}

	if !found {
		stderr, err := env.GetStderr()
		if err != nil {
			t.Errorf("Error getting standard error: %s", err)
		}
		stdout, err := env.GetStdout()
		if err != nil {
			t.Errorf("Error getting standard out: %s", err)
		}
		t.Fatalf("Event wasn't published.\nstderr:\n%s\nstdout:\n%s\n", stderr, stdout)
	}
}

func TestCompressEncryptedDiskQueue(t *testing.T) {
	var config strings.Builder
	queue_path := t.TempDir()

	_, _ = config.WriteString("server:\n")
	_, _ = config.WriteString("  strict_mode: false\n")
	_, _ = config.WriteString("  port: 50052\n")
	_, _ = config.WriteString("  tls: false\n")
	_, _ = config.WriteString("logging:\n")
	_, _ = config.WriteString("  level: debug\n")
	_, _ = config.WriteString("  selectors: [\"*\"]\n")
	_, _ = config.WriteString("  to_stderr: true\n")
	_, _ = config.WriteString("queue:\n")
	_, _ = config.WriteString("  disk:\n")
	_, _ = config.WriteString("    path: " + queue_path + "\n")
	_, _ = config.WriteString("    max_size: 10G\n")
	_, _ = config.WriteString("    use_compression: true\n")
	_, _ = config.WriteString("    use_encryption: true\n")
	_, _ = config.WriteString("    encryption_password: secret\n")

	env, err := NewTestingEnvironment(t, config.String())
	if err != nil {
		t.Fatalf("Error creating environment: %s", err)
	}
	t.Cleanup(func() { env.Stop() })

	found, err := env.WaitUntil("stderr", "gRPC server is ready and is listening on")
	if err != nil {
		t.Fatalf("Error waiting for server to start: %s", err)
	}

	if !found {
		stderr, err := env.GetStderr()
		if err != nil {
			t.Errorf("Error getting standard error: %s", err)
		}
		stdout, err := env.GetStdout()
		if err != nil {
			t.Errorf("Error getting standard out: %s", err)
		}
		t.Fatalf("Test executable failed to start.\nstderr:\n%s\nstdout:\n%s\n", stderr, stdout)
	}

	client, err := env.NewClient("localhost:50052")
	if err != nil {
		t.Fatalf("Error creating client: %s", err)
	}

	unique := "UniqueStringToLookForInOutput"
	events, err := createEvents([]string{unique})
	if err != nil {
		t.Fatalf("error creating events: %s\n", err)
	}

	_, err = client.PublishEvents(env.ctx, &messages.PublishRequest{
		Events: events,
	})
	if err != nil {
		stderr, err := env.GetStderr()
		if err != nil {
			t.Errorf("Error getting standard error: %s", err)
		}
		stdout, err := env.GetStdout()
		if err != nil {
			t.Errorf("Error getting standard out: %s", err)
		}
		t.Fatalf("Error publishing event: %s\nstderr:\n%s\nstdout:\n%s\n", err, stderr, stdout)
	}

	found, err = env.WaitUntil("stdout", unique)
	if err != nil {
		t.Fatalf("Error looking for publish results: %s", err)
	}

	if !found {
		stderr, err := env.GetStderr()
		if err != nil {
			t.Errorf("Error getting standard error: %s", err)
		}
		stdout, err := env.GetStdout()
		if err != nil {
			t.Errorf("Error getting standard out: %s", err)
		}
		t.Fatalf("Event wasn't published.\nstderr:\n%s\nstdout:\n%s\n", stderr, stdout)
	}
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
