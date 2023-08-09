$ErrorActionPreference = "Stop" # set -e
. ($PSScriptRoot + "/pre-install-command.ps1")

Write-Host "--- Setup"
fixCRLF
withGolang $env:GO_VERSION
withMage $env:SETUP_MAGE_VERSION

Write-Host "--- Test"
dir #TODO remove . debug
mage test > test-report.txt
