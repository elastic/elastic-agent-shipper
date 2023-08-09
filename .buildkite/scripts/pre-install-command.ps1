
# Forcing to checkout again all the files with a correct autocrlf.
# Doing this here because we cannot set git clone options before.
function fixCRLF {
    Write-Host "-- Fixing CRLF in git checkout --"
    git config core.autocrlf input
    git rm --quiet --cached -r .
    git reset --quiet --hard
}

function withGolang($version) {
    Write-Host "-- Install golang --"
    choco install -y golang --version $version
    $env:ChocolateyInstall = Convert-Path "$((Get-Command choco).Path)\..\.."
    Import-Module "$env:ChocolateyInstall\helpers\chocolateyProfile.psm1"
    refreshenv
    go version
    go env
}

function withMage($version) {
    Write-Host "-- Install Mage --"
    go mod download -x
    go install github.com/magefile/mage@v$version
}
