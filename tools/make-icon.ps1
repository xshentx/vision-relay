param(
    [string]$ProjectRoot = ""
)

$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($ProjectRoot)) {
    $ProjectRoot = Split-Path -Parent $PSScriptRoot
}

Add-Type -AssemblyName System.Drawing

function New-RoundedRectanglePath {
    param(
        [float]$Size,
        [float]$Radius
    )

    $path = [System.Drawing.Drawing2D.GraphicsPath]::new()
    $diameter = $Radius * 2
    $path.AddArc(0, 0, $diameter, $diameter, 180, 90)
    $path.AddArc($Size - $diameter, 0, $diameter, $diameter, 270, 90)
    $path.AddArc($Size - $diameter, $Size - $diameter, $diameter, $diameter, 0, 90)
    $path.AddArc(0, $Size - $diameter, $diameter, $diameter, 90, 90)
    $path.CloseFigure()
    return $path
}

function New-IconPng {
    param([int]$Size)

    $bitmap = [System.Drawing.Bitmap]::new(
        $Size,
        $Size,
        [System.Drawing.Imaging.PixelFormat]::Format32bppArgb
    )
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    $graphics.Clear([System.Drawing.Color]::Transparent)
    $graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $graphics.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $graphics.CompositingQuality = [System.Drawing.Drawing2D.CompositingQuality]::HighQuality
    $graphics.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::AntiAliasGridFit

    $bounds = [System.Drawing.RectangleF]::new(0, 0, $Size, $Size)
    $path = New-RoundedRectanglePath -Size $Size -Radius ($Size * 0.275)
    $gradient = [System.Drawing.Drawing2D.LinearGradientBrush]::new(
        $bounds,
        [System.Drawing.Color]::FromArgb(255, 39, 75, 142),
        [System.Drawing.Color]::FromArgb(255, 64, 158, 255),
        45
    )
    $graphics.FillPath($gradient, $path)

    $fontFamily = [System.Drawing.FontFamily]::new("Segoe UI")
    $format = [System.Drawing.StringFormat]::new()
    $format.FormatFlags = [System.Drawing.StringFormatFlags]::NoWrap
    $textPath = [System.Drawing.Drawing2D.GraphicsPath]::new()
    $textPath.AddString(
        "VR",
        $fontFamily,
        [int][System.Drawing.FontStyle]::Bold,
        [single]($Size * 0.58),
        [System.Drawing.PointF]::new(0, 0),
        $format
    )
    $textBounds = $textPath.GetBounds()
    $transform = [System.Drawing.Drawing2D.Matrix]::new()
    $transform.Translate(
        [single](($Size - $textBounds.Width) / 2 - $textBounds.X),
        [single](($Size - $textBounds.Height) / 2 - $textBounds.Y)
    )
    $textPath.Transform($transform)
    $textBrush = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::White)
    if ($Size -le 24) {
        $graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::None
        $graphics.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::None
    }
    $graphics.FillPath($textBrush, $textPath)

    $stream = [System.IO.MemoryStream]::new()
    $bitmap.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png)
    $bytes = $stream.ToArray()

    $stream.Dispose()
    $textBrush.Dispose()
    $transform.Dispose()
    $textPath.Dispose()
    $format.Dispose()
    $fontFamily.Dispose()
    $gradient.Dispose()
    $path.Dispose()
    $graphics.Dispose()
    $bitmap.Dispose()
    return ,$bytes
}

function Write-Ico {
    param(
        [string]$Path,
        [int[]]$Sizes
    )

    $entries = foreach ($size in $Sizes) {
        [byte[]]$data = New-IconPng -Size $size
        [pscustomobject]@{
            Size = $size
            Data = $data
        }
    }

    $stream = [System.IO.MemoryStream]::new()
    $writer = [System.IO.BinaryWriter]::new($stream)
    $writer.Write([uint16]0)
    $writer.Write([uint16]1)
    $writer.Write([uint16]$entries.Count)
    $offset = 6 + 16 * $entries.Count

    foreach ($entry in $entries) {
        $dimension = if ($entry.Size -ge 256) { 0 } else { $entry.Size }
        $writer.Write([byte]$dimension)
        $writer.Write([byte]$dimension)
        $writer.Write([byte]0)
        $writer.Write([byte]0)
        $writer.Write([uint16]1)
        $writer.Write([uint16]32)
        $writer.Write([uint32]$entry.Data.Length)
        $writer.Write([uint32]$offset)
        $offset += $entry.Data.Length
    }
    foreach ($entry in $entries) {
        $writer.Write([byte[]]$entry.Data)
    }

    $writer.Flush()
    [System.IO.File]::WriteAllBytes($Path, $stream.ToArray())
    $writer.Dispose()
    $stream.Dispose()
}

$backendAssets = Join-Path $ProjectRoot "backend\internal\server\assets"
$frontendImages = Join-Path $ProjectRoot "frontend\public\assets\images"
[System.IO.Directory]::CreateDirectory($backendAssets) | Out-Null
[System.IO.Directory]::CreateDirectory($frontendImages) | Out-Null

$appSizes = @(16, 20, 24, 28, 32, 40, 48, 64, 96, 128, 256)
$faviconSizes = @(16, 20, 24, 32, 48)
$appIconPath = Join-Path $backendAssets "app.ico"
$appPreviewPath = Join-Path $backendAssets "app.png"
$faviconPath = Join-Path $frontendImages "favicon.ico"

Write-Ico -Path $appIconPath -Sizes $appSizes
Write-Ico -Path $faviconPath -Sizes $faviconSizes
[byte[]]$preview = New-IconPng -Size 256
[System.IO.File]::WriteAllBytes($appPreviewPath, $preview)
