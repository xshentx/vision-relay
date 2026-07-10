param(
    [string]$Output = "vision-relay.exe",
    [switch]$SkipTests
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$outputPath = if ([IO.Path]::IsPathRooted($Output)) {
    $Output
} else {
    Join-Path $projectRoot $Output
}
$tempPath = "$outputPath.tmp"

Push-Location $projectRoot
try {
    if (-not $SkipTests) {
        & go test ./...
        if ($LASTEXITCODE -ne 0) {
            throw "Tests failed; Windows build was not created."
        }
    }

    Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
    & go build -trimpath '-ldflags=-s -w -H=windowsgui' -o $tempPath ./backend/cmd/vision-relay
    if ($LASTEXITCODE -ne 0) {
        throw "Go build failed."
    }

    try {
        Move-Item -LiteralPath $tempPath -Destination $outputPath -Force
    } catch {
        throw "Unable to replace '$outputPath'. Exit the running Vision Relay instance and try again. $($_.Exception.Message)"
    }

    Write-Host "Built Windows GUI executable: $outputPath"
} finally {
    Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
    Pop-Location
}
