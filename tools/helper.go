// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package tools

import (
	"strconv"
	"time"
)

var (
	commit    string
	buildTime string
	qualifier string
	snapshot  string
)

// GetDefaultVersion returns the current libbeat version.
// This method is in a separate file as the version.go file is auto generated
func GetDefaultVersion() string {
	if qualifier == "" {
		return DefaultBeatVersion
	}
	return DefaultBeatVersion + "-" + qualifier
}

// BuildTime exposes the compile-time build time information.
// It will represent the zero time instant if parsing fails.
func BuildTime() time.Time {
	t, err := time.Parse(time.RFC3339, buildTime)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Commit exposes the compile-time commit hash.
func Commit() string {
	return commit
}

// Snapshot returns true if binary was compiled as a SNAPSHOT build
func Snapshot() bool {
	val, err := strconv.ParseBool(snapshot)
	return err == nil && val
}
