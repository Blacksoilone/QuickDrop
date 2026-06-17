# Make icon files from SVG source.
# Pipeline: Inkscape (SVG->PNG 256x256) -> ImageMagick (PNG->ICO multi-resolution)

param(
    [string]$SourceSvg = "assets/icon-source.svg",
    [string]$MainIco = "assets/icon.ico",
    [string]$TrayIco = "internal/tray/icon.ico",
    [string]$AlertIco = "internal/tray/icon-alert.ico"
)

$ErrorActionPreference = "Stop"

$inkscape = "$env:USERPROFILE\scoop\apps\inkscape\current\bin\inkscape.exe"
$magick = "$env:USERPROFILE\scoop\apps\imagemagick-lean\current\magick.exe"

if (-not (Test-Path $inkscape)) {
    Write-Host "Inkscape not installed. Run: scoop install inkscape" -ForegroundColor Red
    exit 1
}
if (-not (Test-Path $magick)) {
    Write-Host "ImageMagick-lean not installed. Run: scoop install imagemagick-lean" -ForegroundColor Red
    exit 1
}
if (-not (Test-Path $SourceSvg)) {
    Write-Host "Source not found: $SourceSvg" -ForegroundColor Red
    exit 1
}

Write-Host "=== Make QuickDrop icons ===" -ForegroundColor Cyan
Write-Host "Source: $SourceSvg" -ForegroundColor DarkGray

# 1. Inkscape: SVG -> PNG 256x256
$tempPng = "$env:TEMP\quickdrop-icon-256.png"
Write-Host "Step 1: Inkscape SVG -> PNG 256x256 ..."
$absSvg = (Resolve-Path $SourceSvg).Path
& $inkscape --export-type=png --export-filename=$tempPng --export-width=256 --export-height=256 $absSvg 2>&1 | Out-Null
if (-not (Test-Path $tempPng)) {
    Write-Host "Inkscape PNG render failed" -ForegroundColor Red
    exit 1
}

# 2. ImageMagick: PNG -> multi-resolution ICO
Write-Host "Step 2: ImageMagick PNG -> multi-resolution ICO ..."
& $magick $tempPng `
    -define "icon:auto-resize=16,24,32,48,64,128,256" `
    $MainIco
if ($LASTEXITCODE -ne 0) {
    Write-Host "Main icon failed" -ForegroundColor Red
    exit 1
}

# 3. Copy as tray icon
Copy-Item -Path $MainIco -Destination $TrayIco -Force

# 4. Alert icon: main icon + red dot top-right
$alertPng = "$env:TEMP\quickdrop-alert.png"
Write-Host "Step 3: Build alert PNG with red dot ..."
& $magick $tempPng `
    -fill "#dc3545" -stroke "#ffffff" -strokewidth 4 `
    -draw "circle 200,56 200,8" `
    $alertPng
if ($LASTEXITCODE -ne 0) {
    Write-Host "Alert PNG failed" -ForegroundColor Red
    exit 1
}

Write-Host "Step 4: Build alert ICO ..."
& $magick $alertPng `
    -define "icon:auto-resize=16,24,32,48,64,128,256" `
    $AlertIco
if ($LASTEXITCODE -ne 0) {
    Write-Host "Alert ICO failed" -ForegroundColor Red
    exit 1
}

Remove-Item $tempPng, $alertPng -Force -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "Done. Generated files:" -ForegroundColor Green
Get-Item $MainIco, $TrayIco, $AlertIco | Format-Table Name, Length, FullName -AutoSize