param(
    [string]$Output = "vision-relay.exe",
    [switch]$SkipTests,
    [string]$Version = "",
    [string]$SigningCertificatePath = $env:WINDOWS_SIGNING_CERTIFICATE_PATH,
    [string]$SigningCertificatePassword = $env:WINDOWS_SIGNING_CERTIFICATE_PASSWORD,
    [string]$TimestampUrl = "http://timestamp.digicert.com",
    [switch]$RequireSignature,
    [string]$UpdateSigningPrivateKeyPath = $env:UPDATE_SIGNING_PRIVATE_KEY_PATH,
    [switch]$RequireUpdateSignature
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
$iconResourcePath = Join-Path $iconDirectory "app_windows.syso"
$generatedResourcePath = Join-Path $iconDirectory "app.generated.rc"
if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = (& git -C $projectRoot describe --tags --always --dirty 2>$null)
    if ([string]::IsNullOrWhiteSpace($Version)) { $Version = "dev" }
}
if ($RequireSignature -and [string]::IsNullOrWhiteSpace($SigningCertificatePath)) {
    throw "A release signature is required. Set WINDOWS_SIGNING_CERTIFICATE_PATH or pass -SigningCertificatePath."
}
if ($RequireUpdateSignature -and [string]::IsNullOrWhiteSpace($UpdateSigningPrivateKeyPath)) {
    throw "An Ed25519 update signature is required."
}
if ($UpdateSigningPrivateKeyPath -and -not (Test-Path -LiteralPath $UpdateSigningPrivateKeyPath -PathType Leaf)) {
    throw "Update signing key file not found."
}
if (-not [string]::IsNullOrWhiteSpace($SigningCertificatePath) -and -not (Test-Path -LiteralPath $SigningCertificatePath -PathType Leaf)) {
    throw "Signing certificate not found: $SigningCertificatePath"
}

function Get-NumericFileVersion {
    param([string]$ReleaseVersion)
    $numbers = [regex]::Matches($ReleaseVersion, '\d+') | ForEach-Object { [Math]::Min([int]$_.Value, 65535) }
    $parts = @(0, 0, 0, 0)
    for ($i = 0; $i -lt [Math]::Min($numbers.Count, 4); $i++) {
        $parts[$i] = $numbers[$i]
    }
    return $parts
}

function Write-WindowsVersionResource {
    param([string]$Path, [string]$ReleaseVersion)
    $parts = Get-NumericFileVersion -ReleaseVersion $ReleaseVersion
    $numeric = $parts -join ','
    $resource = @"
#include <windows.h>
32512 ICON "../../internal/server/assets/app.ico"

1 VERSIONINFO
FILEVERSION $numeric
PRODUCTVERSION $numeric
FILEFLAGSMASK 0x3fL
FILEFLAGS 0x0L
FILEOS VOS_NT_WINDOWS32
FILETYPE VFT_APP
FILESUBTYPE 0x0L
BEGIN
  BLOCK "StringFileInfo"
  BEGIN
    BLOCK "040904b0"
    BEGIN
      VALUE "CompanyName", "Vision Relay Contributors"
      VALUE "FileDescription", "Vision Relay Desktop"
      VALUE "FileVersion", "$ReleaseVersion"
      VALUE "InternalName", "vision-relay"
      VALUE "LegalCopyright", "Vision Relay Contributors"
      VALUE "OriginalFilename", "vision-relay.exe"
      VALUE "ProductName", "Vision Relay"
      VALUE "ProductVersion", "$ReleaseVersion"
    END
  END
  BLOCK "VarFileInfo"
  BEGIN
    VALUE "Translation", 0x0409, 1200
  END
END
"@
    [IO.File]::WriteAllText($Path, $resource, [Text.UTF8Encoding]::new($false))
}

function Find-SignTool {
    $command = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($null -ne $command) { return $command.Source }
    $kitsRoot = Join-Path ${env:ProgramFiles(x86)} "Windows Kits\10\bin"
    if (Test-Path -LiteralPath $kitsRoot) {
        $candidate = Get-ChildItem -LiteralPath $kitsRoot -Filter signtool.exe -Recurse -ErrorAction SilentlyContinue |
            Where-Object { $_.FullName -match '\\x64\\signtool\.exe$' } |
            Sort-Object FullName -Descending |
            Select-Object -First 1
        if ($null -ne $candidate) { return $candidate.FullName }
    }
    return $null
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
    Write-WindowsVersionResource -Path $generatedResourcePath -ReleaseVersion $Version
    $windres = Get-Command windres -ErrorAction SilentlyContinue
    if ($null -ne $windres) {
        Push-Location $iconDirectory
        try {
            & $windres.Source --target=pe-x86-64 --input=$generatedResourcePath --output=$iconResourcePath --output-format=coff
            if ($LASTEXITCODE -ne 0) {
                throw "Unable to embed the Windows icon and version information."
            }
        } finally {
            Pop-Location
        }
    } else {
        throw "windres was not found. Install MinGW-w64; refusing to build with stale Windows version metadata."
    }

    $updatePublicKey = ""
    if ($UpdateSigningPrivateKeyPath) {
        $updatePublicKey = (& go run ./tools/sign-update -key $UpdateSigningPrivateKeyPath -print-public-key)
        if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($updatePublicKey)) {
            throw "Unable to derive the Ed25519 update public key."
        }
        $updatePublicKey = $updatePublicKey.Trim()
    }
    $linkerFlags = "-s -w -H=windowsgui -X=vision-relay/backend/internal/server.Version=$Version"
    if ($updatePublicKey) {
        $linkerFlags += " -X=vision-relay/backend/internal/server.UpdatePublicKey=$updatePublicKey"
    }
    & go build -trimpath "-ldflags=$linkerFlags" -o $tempPath ./backend/cmd/vision-relay
    if ($LASTEXITCODE -ne 0) {
        throw "Go build failed."
    }

    if (-not [string]::IsNullOrWhiteSpace($SigningCertificatePath)) {
        if (-not (Test-Path -LiteralPath $SigningCertificatePath -PathType Leaf)) {
            throw "Signing certificate not found: $SigningCertificatePath"
        }
        $signTool = Find-SignTool
        if ([string]::IsNullOrWhiteSpace($signTool)) {
            throw "signtool.exe was not found. Install the Windows SDK before signing."
        }
        $signArgs = @("sign", "/fd", "SHA256", "/td", "SHA256", "/tr", $TimestampUrl, "/d", "Vision Relay", "/f", $SigningCertificatePath)
        if (-not [string]::IsNullOrEmpty($SigningCertificatePassword)) {
            $signArgs += @("/p", $SigningCertificatePassword)
        }
        $signArgs += $tempPath
        & $signTool @signArgs
        if ($LASTEXITCODE -ne 0) {
            throw "Authenticode signing failed."
        }
        $signature = Get-AuthenticodeSignature -LiteralPath $tempPath
        if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::Valid) {
            throw "Authenticode verification failed: $($signature.StatusMessage)"
        }
        Write-Host "Authenticode signature verified: $($signature.SignerCertificate.Subject)"
    } elseif ($RequireSignature) {
        throw "A release signature is required. Set WINDOWS_SIGNING_CERTIFICATE_PATH or pass -SigningCertificatePath."
    } else {
        Write-Warning "The executable is unsigned. Public Windows releases should be Authenticode-signed to reduce security-software false positives."
    }

    try {
        Move-Item -LiteralPath $tempPath -Destination $outputPath -Force
    } catch {
        throw "Unable to replace '$outputPath'. Exit the running Vision Relay instance and try again. $($_.Exception.Message)"
    }

    $hash = (Get-FileHash -LiteralPath $outputPath -Algorithm SHA256).Hash.ToLowerInvariant()
    Set-Content -LiteralPath "$outputPath.sha256" -Value "$hash  $([IO.Path]::GetFileName($outputPath))" -Encoding ascii
    # Never leave a signature from an earlier build beside a different binary.
    [IO.File]::Delete("$outputPath.sig")
    if ($UpdateSigningPrivateKeyPath) {
        & go run ./tools/sign-update -key $UpdateSigningPrivateKeyPath -input $outputPath -output "$outputPath.sig"
        if ($LASTEXITCODE -ne 0) {
            throw "Ed25519 update signing failed."
        }
        Write-Host "Ed25519 update signature: $outputPath.sig"
    } elseif ($RequireUpdateSignature) {
        throw "An Ed25519 update signature is required."
    } else {
        Write-Warning "No Ed25519 update key was provided; automatic update is disabled in this build."
    }
    Write-Host "Built Windows GUI executable: $outputPath (version $Version)"
    Write-Host "SHA-256: $outputPath.sha256"
} finally {
    Remove-Item -LiteralPath $tempPath -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $generatedResourcePath -Force -ErrorAction SilentlyContinue
    Pop-Location
}
