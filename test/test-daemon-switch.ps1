# Phase 2.2 + 2.3 验收: daemon + IPC 切换文件.
#
# 跑法: .\test\test-daemon-switch.ps1
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
if (-not (Test-Path $testPng)) {
    Write-Host "FAIL: $testPng 不存在" -ForegroundColor Red
    exit 1
}
if (-not (Test-Path $cnPng)) {
    Write-Host "FAIL: $cnPng 不存在, 先跑 .\test\prepare-fixtures.ps1" -ForegroundColor Red
    exit 1
}

# 防止以前残留的 daemon 占着 8443
Get-Process quickdrop -ErrorAction SilentlyContinue | ForEach-Object {
    Write-Host "WARN: 清理残留 daemon PID $($_.Id)" -ForegroundColor Yellow
    Stop-Process -Id $_.Id -Force
}
Start-Sleep -Seconds 1

$fail = 0
function Check($name, $cond) {
    if ($cond) {
        Write-Host "  PASS: $name" -ForegroundColor Green
    } else {
        Write-Host "  FAIL: $name" -ForegroundColor Red
        $script:fail++
    }
}

Write-Host ""
Write-Host "=== Step 1: start daemon with test.png ===" -ForegroundColor Cyan
$daemon = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 2
Check "daemon 进程存活" (-not $daemon.HasExited)
if ($daemon.HasExited) {
    Write-Host "daemon 死了, 看 %TEMP%\quickdrop.log" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "=== Step 2: probe /internal/health ===" -ForegroundColor Cyan
try {
    $h = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/internal/health -TimeoutSec 3
    Check "/internal/health 返回 200" ($h.StatusCode -eq 200)
    Check "X-QuickDrop: 1 header" ($h.Headers."X-QuickDrop" -eq "1")
} catch {
    Check "/internal/health 可达" $false
}

Write-Host ""
Write-Host "=== Step 3: LAN /internal/* 应被 requireLocal 拒绝 ===" -ForegroundColor Cyan
# 从日志读 LAN IP
$lanIP = ((Get-Content "$env:TEMP\quickdrop.log" -ErrorAction SilentlyContinue) -join "`n" | Select-String -Pattern "http://(\d+\.\d+\.\d+\.\d+):8443" -AllMatches).Matches | ForEach-Object { $_.Groups[1].Value } | Select-Object -First 1
if ($lanIP -and $lanIP -ne "127.0.0.1") {
    try {
        $h2 = Invoke-WebRequest -UseBasicParsing "http://$lanIP`:8443/internal/health" -TimeoutSec 3
        Check "LAN /internal/health 被拒 (got $($h2.StatusCode))" $false
    } catch {
        $code = $_.Exception.Response.StatusCode.value__
        Check "LAN /internal/health 返回 404" ($code -eq 404)
    }
} else {
    Write-Host "  SKIP: 无法检测 LAN IP" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "=== Step 4: GET / 显示 test.png ===" -ForegroundColor Cyan
$html1 = (Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/).Content
Check "主页含 'test.png'" ($html1 -match "test\.png")

Write-Host ""
Write-Host "=== Step 5: 第二次 send 走客户端模式 (切到中文名文件) ===" -ForegroundColor Cyan
$sw = [System.Diagnostics.Stopwatch]::StartNew()
$client = Start-Process -FilePath $exe -ArgumentList "send",$cnPng -PassThru -WindowStyle Hidden -Wait
$sw.Stop()
Check "客户端退出码 0" ($client.ExitCode -eq 0)
Check "客户端 < 3 秒返回 (实际 $($sw.ElapsedMilliseconds)ms)" ($sw.ElapsedMilliseconds -lt 3000)

Write-Host ""
Write-Host "=== Step 6: 主页应已切换 ===" -ForegroundColor Cyan
$html2 = (Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/).Content
Check "主页切到 '你好世界.png'" ($html2 -match "你好世界\.png")
Check "主页不再有 'test.png'" (-not ($html2 -match "<p class=`"name`">test\.png"))

Write-Host ""
Write-Host "=== Step 7: /file disposition 也切换 ===" -ForegroundColor Cyan
$f = Invoke-WebRequest -UseBasicParsing -Method Head http://127.0.0.1:8443/file
$dispOK = $f.Headers."Content-Disposition" -match "%E4%BD%A0%E5%A5%BD%E4%B8%96%E7%95%8C\.png"
Check "Content-Disposition 含 你好世界.png 的 UTF-8 编码" $dispOK

Write-Host ""
Write-Host "=== Step 8: 第三次 send 切回 test.png ===" -ForegroundColor Cyan
$client3 = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden -Wait
Check "第三次 send 退出码 0" ($client3.ExitCode -eq 0)
$html3 = (Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/).Content
Check "主页切回 test.png" ($html3 -match "test\.png")

Write-Host ""
Write-Host "=== Step 9: daemon 经历 3 次切换仍存活 ===" -ForegroundColor Cyan
Check "daemon PID $($daemon.Id) 仍在跑" ($null -ne (Get-Process -Id $daemon.Id -ErrorAction SilentlyContinue))

Write-Host ""
Write-Host "=== Cleanup ===" -ForegroundColor Cyan
Stop-Process -Id $daemon.Id -Force -ErrorAction SilentlyContinue

Write-Host ""
if ($fail -eq 0) {
    Write-Host "ALL PASS" -ForegroundColor Green
    exit 0
} else {
    Write-Host "FAILED: $fail check(s)" -ForegroundColor Red
    exit 1
}
