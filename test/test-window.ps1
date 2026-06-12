# Phase 2.10 验收: webview 子进程 + window-mode 三种策略.
#
# 跑法: .\test\test-window.ps1
# 退出码: 0 PASS, 1 FAIL

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $root "quickdrop.exe"
$testPng = Join-Path $root "test.png"
$cnPng = Join-Path $root "test\你好世界.png"

if (-not (Test-Path $exe)) {
    Write-Host "FAIL: $exe 不存在, 先跑 .\build.ps1" -ForegroundColor Red
    exit 1
}
if (-not (Test-Path $cnPng)) {
    Write-Host "FAIL: $cnPng 不存在, 先跑 .\test\prepare-fixtures.ps1" -ForegroundColor Red
    exit 1
}

$fail = 0
function Check($name, $cond) {
    if ($cond) {
        Write-Host "  PASS: $name" -ForegroundColor Green
    } else {
        Write-Host "  FAIL: $name" -ForegroundColor Red
        $script:fail++
    }
}

function CleanAll {
    Get-Process quickdrop -ErrorAction SilentlyContinue | ForEach-Object {
        Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
    }
    Start-Sleep -Seconds 1
}

function CountProcs {
    @(Get-Process quickdrop -ErrorAction SilentlyContinue).Count
}

CleanAll

# Sanity: window 子命令独立可跑 (不依赖 daemon)
Write-Host ""
Write-Host "=== Sanity: 'window' subcommand standalone ===" -ForegroundColor Cyan
$sub = Start-Process -FilePath $exe -ArgumentList "window","about:blank" -PassThru
Start-Sleep -Seconds 3
Check "window 子进程存活 (PID $($sub.Id))" (-not $sub.HasExited)
if (-not $sub.HasExited) { Stop-Process -Id $sub.Id -Force }
CleanAll

# Test 1: replace mode
Write-Host ""
Write-Host "=== Test 1: mode=replace, 切换文件 → 始终 1 个 window ===" -ForegroundColor Cyan
$env:QUICKDROP_WINDOW_MODE = "replace"
$daemon = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3
Check "send #1 后: 2 进程 (1 daemon + 1 window)" ((CountProcs) -eq 2)
& $exe send $cnPng | Out-Null
Start-Sleep -Seconds 3
Check "send #2 后仍是 2 进程 (replace 杀了旧 window)" ((CountProcs) -eq 2)
& $exe send $testPng | Out-Null
Start-Sleep -Seconds 3
Check "send #3 后仍是 2 进程" ((CountProcs) -eq 2)
CleanAll

# Test 2: keep mode
Write-Host ""
Write-Host "=== Test 2: mode=keep, 3 次 send → 1 daemon + 3 windows ===" -ForegroundColor Cyan
$env:QUICKDROP_WINDOW_MODE = "keep"
$daemon = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3
Check "send #1 后: 2 进程" ((CountProcs) -eq 2)
& $exe send $cnPng | Out-Null
Start-Sleep -Seconds 2
Check "send #2 后: 3 进程 (keep 留着旧 window)" ((CountProcs) -eq 3)
& $exe send $testPng | Out-Null
Start-Sleep -Seconds 3
Check "send #3 后: 4 进程" ((CountProcs) -eq 4)
CleanAll

# Test 3: first-only mode
Write-Host ""
Write-Host "=== Test 3: mode=first-only, 只首次 send 开窗 ===" -ForegroundColor Cyan
$env:QUICKDROP_WINDOW_MODE = "first-only"
$daemon = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3
Check "send #1 后: 2 进程 (1 daemon + 1 window)" ((CountProcs) -eq 2)
& $exe send $cnPng | Out-Null
Start-Sleep -Seconds 2
Check "send #2 后仍是 2 进程 (first-only 不再开新窗)" ((CountProcs) -eq 2)
& $exe send $testPng | Out-Null
Start-Sleep -Seconds 2
Check "send #3 后仍是 2 进程" ((CountProcs) -eq 2)
CleanAll

Remove-Item Env:\QUICKDROP_WINDOW_MODE -ErrorAction SilentlyContinue

Write-Host ""
if ($fail -eq 0) {
    Write-Host "ALL PASS" -ForegroundColor Green
    exit 0
} else {
    Write-Host "FAILED: $fail check(s)" -ForegroundColor Red
    exit 1
}
