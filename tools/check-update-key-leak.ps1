[CmdletBinding()]
param(
    [ValidateSet("Staged", "Tracked", "Paths")]
    [string]$Scope = "Tracked",
    [string]$KeyFile = "",
    [string[]]$Paths = @(),
    [switch]$RequireKnownKey
)

$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

function Invoke-GitLines {
    param([string[]]$Arguments)
    $output = @(& git -c core.quotepath=false -C $repoRoot @Arguments)
    if ($LASTEXITCODE -ne 0) {
        throw "git $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
    return $output
}

function Read-CandidateText {
    param([string]$Path, [bool]$FromIndex)
    if ($FromIndex) {
        $content = @(& git -c core.quotepath=false -C $repoRoot show ":$Path")
        if ($LASTEXITCODE -ne 0) {
            throw "could not inspect staged file: $Path"
        }
        return ($content -join "`n")
    }
    $absolutePath = if ([IO.Path]::IsPathRooted($Path)) { $Path } else { Join-Path $repoRoot $Path }
    if (-not (Test-Path -LiteralPath $absolutePath -PathType Leaf)) {
        return ""
    }
    $bytes = [IO.File]::ReadAllBytes($absolutePath)
    return [Text.Encoding]::GetEncoding(28591).GetString($bytes)
}

$fromIndex = $Scope -eq "Staged"
switch ($Scope) {
    "Staged" { $candidates = @(Invoke-GitLines @("diff", "--cached", "--name-only", "--diff-filter=ACMR")) }
    "Tracked" { $candidates = @(Invoke-GitLines @("ls-files")) }
    "Paths" { $candidates = @($Paths) }
}
$candidates = @($candidates | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Select-Object -Unique)

$blockedPathPattern = '(?i)(^|[\\/])\.vision-relay-secrets([\\/]|$)|\.(key|pem|pfx|p12|jks|keystore)$'
$blockedPaths = if ($Scope -eq "Paths") { @() } else { @($candidates | Where-Object { $_ -match $blockedPathPattern }) }
if ($blockedPaths.Count -gt 0) {
    foreach ($path in $blockedPaths) {
        Write-Error "Refusing to include private-key path in Git: $path"
    }
    exit 1
}

$keyMaterial = ""
if (-not [string]::IsNullOrWhiteSpace($KeyFile)) {
    $resolvedKeyFile = if ([IO.Path]::IsPathRooted($KeyFile)) { $KeyFile } else { Join-Path $repoRoot $KeyFile }
    if (Test-Path -LiteralPath $resolvedKeyFile -PathType Leaf) {
        $keyMaterial = ([IO.File]::ReadAllText($resolvedKeyFile, [Text.Encoding]::ASCII)).Trim()
    } elseif ($RequireKnownKey) {
        throw "Required local update signing key was not found"
    }
} elseif (-not [string]::IsNullOrWhiteSpace($env:UPDATE_SIGNING_PRIVATE_KEY_B64)) {
    $keyMaterial = $env:UPDATE_SIGNING_PRIVATE_KEY_B64.Trim()
}

if (-not [string]::IsNullOrWhiteSpace($keyMaterial)) {
    try {
        $decoded = [Convert]::FromBase64String($keyMaterial)
        if ($decoded.Length -ne 32 -and $decoded.Length -ne 64) {
            throw "unexpected decoded length"
        }
    } catch {
        throw "Configured update signing key is not a valid Ed25519 seed/private key"
    } finally {
        if ($null -ne $decoded) {
            [Array]::Clear($decoded, 0, $decoded.Length)
        }
    }
} elseif ($RequireKnownKey) {
    throw "UPDATE_SIGNING_PRIVATE_KEY_B64 is required for exact leak detection"
}

$leaks = New-Object System.Collections.Generic.List[string]
foreach ($path in $candidates) {
    $content = Read-CandidateText -Path $path -FromIndex $fromIndex
    if (-not [string]::IsNullOrWhiteSpace($keyMaterial) -and $content.IndexOf($keyMaterial, [StringComparison]::Ordinal) -ge 0) {
        $leaks.Add($path)
        continue
    }
    if ($content -match '(?i)UPDATE_SIGNING_PRIVATE_KEY_B64\s*[:=]\s*[A-Za-z0-9+/]{43}=') {
        $leaks.Add($path)
    }
}
$keyMaterial = ""

if ($leaks.Count -gt 0) {
    foreach ($path in $leaks) {
        Write-Error "Update signing private key material detected in: $path"
    }
    exit 1
}

Write-Host "Update signing key leak check passed ($Scope, $($candidates.Count) files)."