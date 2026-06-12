# QuickDrop build script (Windows native).
#
# 用法:
#   .\build.ps1            正常构建 (隐藏黑窗, 适合 release)
#   .\build.ps1 -Debug     带控制台输出, 适合调试 (stderr 直接可见)
#   .\build.ps1 -Run       构建后跑 .\quickdrop.exe send test.png
#   .\build.ps1 -SkipWeb   跳过前端 (Vue) 构建, 只重编 Go (前端没改时省时间)
#
# 输出: D:\go_workspace\QuickDrop\quickdrop.exe
#
# 工作流:
#   1. (web/) npm run build → web/dist/{index,d,r,u}.html + assets/*
#   2. 复制 web/dist/ → cmd/quickdrop/web/ (go:embed 不允许 ../)
#   3. go build → quickdrop.exe (含 embed 的 Vue 产物)

param(
    [switch]$Debug,
    [switch]$Run,
    [switch]$SkipWeb
)

$ErrorActionPreference = "Stop"

# PATH: Go + mingw (CGO) + Node + scoop shims.
$env:PATH = "$env:USERPROFILE\scoop\apps\mingw\current\bin;$env:USERPROFILE\scoop\shims;C:\Program Files\Go\bin;$env:USERPROFILE\AppData\Roaming\npm;C:\Program Files\nodejs;" + $env:PATH

if (-not $SkipWeb) {
    Write-Host "=== Vue build ===" -ForegroundColor Cyan
    Push-Location web
    try {
        if (-not (Test-Path node_modules)) {
            Write-Host "node_modules 不存在, 先 npm install..."
            npm install --no-audit --no-fund
            if ($LASTEXITCODE -ne 0) { Write-Host "npm install FAILED" -ForegroundColor Red; exit 1 }
        }
        npm run build
        if ($LASTEXITCODE -ne 0) { Write-Host "vue build FAILED" -ForegroundColor Red; exit 1 }
    } finally {
        Pop-Location
    }

    # 复制 web/dist/ → cmd/quickdrop/web/ (go:embed 不允许 ../)
    $embedDir = "cmd\quickdrop\web"
    if (Test-Path $embedDir) {
        Remove-Item -Path $embedDir -Recurse -Force
    }
    New-Item -ItemType Directory -Path $embedDir -Force | Out-Null
    Copy-Item -Path "web\dist\*" -Destination $embedDir -Recurse -Force
    Write-Host "复制 web/dist → $embedDir 完成" -ForegroundColor Green
} else {
    Write-Host "=== 跳过 Vue build (-SkipWeb) ===" -ForegroundColor Yellow
    if (-not (Test-Path "cmd\quickdrop\web\index.html")) {
        Write-Host "WARN: cmd/quickdrop/web/ 里没有上次构建产物, 至少跑一次不带 -SkipWeb" -ForegroundColor Yellow
        exit 1
    }
}

$ldflags = if ($Debug) { "" } else { "-H=windowsgui" }
$mode = if ($Debug) { "DEBUG (console visible)" } else { "RELEASE (windowsgui)" }

Write-Host ""
Write-Host "=== Go build [$mode] ===" -ForegroundColor Cyan
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
