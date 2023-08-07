#!/bin/bash

set -euo pipefail

echo "--- Pre install"
source .buildkite/scripts/pre-install-command.sh
add_bin_path
with_go_junit_report

# Create Junit report for junit annotation plugin
buildkite-agent artifact download tests-report.txt . --step test
go-junit-report > junit-report.xml < tests-report.txt
