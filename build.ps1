# QuickDrop build script (Windows native).
#
# 用法:
#   .\build.ps1            正常构建 (隐藏黑窗, 适合 release)
#   .\build.ps1 -Debug     带控制台输出, 适合调试 (stderr 直接可见)
#   .\build.ps1 -Run       构建后跑 .\quickdrop.exe send test.png
#
# 输出: D:\go_workspace\QuickDrop\quickdrop.exe

param(
    [switch]$Debug,
    [switch]$Run
)

$ErrorActionPreference = "Stop"

# PATH: Go + mingw (CGO) + scoop shims.
# Session 重启后用户级 PATH 已固化, 但有些场景 (脚本子进程) 拿不到, 显式拼一下保险.
$env:PATH = "$env:USERPROFILE\scoop\apps\mingw\current\bin;$env:USERPROFILE\scoop\shims;C:\Program Files\Go\bin;" + $env:PATH

$ldflags = if ($Debug) { "" } else { "-H=windowsgui" }
$mode = if ($Debug) { "DEBUG (console visible)" } else { "RELEASE (windowsgui)" }

Write-Host "QuickDrop build [$mode]" -ForegroundColor Cyan
Write-Host "  go: $(go version)"
Write-Host "  gcc: $((gcc --version | Select-Object -First 1))"

if ($ldflags) {
    go build -ldflags="$ldflags" -o quickdrop.exe ./cmd/quickdrop
} else {
    go build -o quickdrop.exe ./cmd/quickdrop
}

if ($LASTEXITCODE -ne 0) {
    Write-Host "BUILD FAILED" -ForegroundColor Red
    exit 1
}

$exe = Get-Item .\quickdrop.exe
Write-Host "BUILD OK: $($exe.Name) $([Math]::Round($exe.Length / 1MB, 2)) MB" -ForegroundColor Green

if ($Run) {
    if (-not (Test-Path .\test.png)) {
        Write-Host "test.png not found, skipping -Run" -ForegroundColor Yellow
        exit 0
    }
    Write-Host "Running: .\quickdrop.exe send test.png" -ForegroundColor Cyan
    .\quickdrop.exe send test.png
}
