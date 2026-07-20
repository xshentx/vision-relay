param(
    [string]$Output = "vision-relay.exe",
    [switch]$SkipTests,
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$outputPath = if ([IO.Path]::IsPathRooted($Output)) {
    $Output
} else {
    Join-Path $projectRoot $Output
}
$tempPath = "$outputPath.tmp"
$iconDirectory = Join-Path $projectRoot "backend\cmd\vision-relay"
$iconGeneratorPath = Join-Path $projectRoot "tools\make-icon.ps1"
$iconDefinitionPath = Join-Path $iconDirectory "app.rc"
$iconResourcePath = Join-Path $iconDirectory "app.syso"
if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = (& git -C $projectRoot describe --tags --always --dirty 2>$null)
    if ([string]::IsNullOrWhiteSpace($Version)) { $Version = "dev" }
}

Push-Location $projectRoot
try {
    if (-not $SkipTests) {
        & go test ./...
        if ($LASTEXITCODE -ne 0) {
            throw "Tests failed; Windows build was not created."
        }
    }

    Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
    & $iconGeneratorPath -ProjectRoot $projectRoot
    $windres = Get-Command windres -ErrorAction SilentlyContinue
    if ($null -ne $windres) {
        Push-Location $iconDirectory
        try {
            & $windres.Source --target=pe-x86-64 --input=$iconDefinitionPath --output=$iconResourcePath --output-format=coff
            if ($LASTEXITCODE -ne 0) {
                throw "Unable to embed the Windows application icon."
            }
        } finally {
            Pop-Location
        }
    } else {
        Write-Warning "windres was not found; using the existing Windows icon resource."
    }
    & go build -trimpath "-ldflags=-s -w -H=windowsgui -X=vision-relay/backend/internal/server.Version=$Version" -o $tempPath ./backend/cmd/vision-relay
    if ($LASTEXITCODE -ne 0) {
        throw "Go build failed."
    }

    try {
        Move-Item -LiteralPath $tempPath -Destination $outputPath -Force
    } catch {
        throw "Unable to replace '$outputPath'. Exit the running Vision Relay instance and try again. $($_.Exception.Message)"
    }

    $hash = (Get-FileHash -LiteralPath $outputPath -Algorithm SHA256).Hash.ToLowerInvariant()
    Set-Content -LiteralPath "$outputPath.sha256" -Value "$hash  $([IO.Path]::GetFileName($outputPath))" -Encoding ascii
    Write-Host "Built Windows GUI executable: $outputPath (version $Version)"
    Write-Host "SHA-256: $outputPath.sha256"
} finally {
    Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
    Pop-Location
}
