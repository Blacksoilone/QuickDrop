# Phase 2.13 验收: 接收模式独立入口 + 安全分离.
#
# 跑法: .\test\test-receive.ps1
# 退出码: 0 PASS, 1 FAIL

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $root "quickdrop.exe"
$testPng = Join-Path $root "test.png"

if (-not (Test-Path $exe)) {
    Write-Host "FAIL: $exe 不存在, 先跑 .\build.ps1" -ForegroundColor Red
    exit 1
}

Get-Process quickdrop -ErrorAction SilentlyContinue | ForEach-Object {
    Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
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

function StatusOf($url) {
    try {
        $r = Invoke-WebRequest -UseBasicParsing $url -TimeoutSec 3
        return $r.StatusCode
    } catch {
        return $_.Exception.Response.StatusCode.value__
    }
}

# Test 1: 纯接收模式启动 (无文件) — 发送类路由全 404, /r 可达, /u 默认 404
Write-Host ""
Write-Host "=== Test 1: quickdrop recv (无文件), 发送路由全 404, /r 可达 ===" -ForegroundColor Cyan
$d = Start-Process -FilePath $exe -ArgumentList "recv" -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3
if ($d.HasExited) {
    Write-Host "FAIL: daemon 死了" -ForegroundColor Red
    Get-Content "$env:TEMP\quickdrop.log" | Select-Object -Last 20
    exit 1
}
Check "daemon 存活 (PID $($d.Id))" (-not $d.HasExited)
Check "/ (发送 dashboard) 无文件时 404" ((StatusOf 'http://127.0.0.1:8443/') -eq 404)
Check "/d (发送目标) 无文件时 404" ((StatusOf 'http://127.0.0.1:8443/d') -eq 404)
Check "/file 无文件时 404" ((StatusOf 'http://127.0.0.1:8443/file') -eq 404)
Check "/qr 无文件时 404" ((StatusOf 'http://127.0.0.1:8443/qr') -eq 404)
Check "/r (接收 dashboard) 可达 200" ((StatusOf 'http://127.0.0.1:8443/r') -eq 200)
Check "/qr-recv 可达 200" ((StatusOf 'http://127.0.0.1:8443/qr-recv') -eq 200)
# recv 模式启动时 EnableReceive(true) → /u 应该 200
$status = StatusOf 'http://127.0.0.1:8443/u'
Check "/u (上传表单) 接收开启时 200 (实际 $status)" ($status -eq 200)

Write-Host ""
Write-Host "=== Test 2: 用 IPC 关接收 → /u 应回 404 ===" -ForegroundColor Cyan
$resp = Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/internal/receive -Body "off"
Check "/internal/receive off 返回 200" ($resp.StatusCode -eq 200)
Start-Sleep -Milliseconds 300
Check "/u 接收关闭后 404" ((StatusOf 'http://127.0.0.1:8443/u') -eq 404)
Check "/r 仍可达 200 (不受 receiveMode 门禁)" ((StatusOf 'http://127.0.0.1:8443/r') -eq 200)

Write-Host ""
Write-Host "=== Test 3: 重新 IPC 开接收 → /u 又 200, 上传文件验证落盘 ===" -ForegroundColor Cyan
$resp = Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/internal/receive -Body "on"
Check "/internal/receive on 返回 200" ($resp.StatusCode -eq 200)
Start-Sleep -Milliseconds 300
Check "/u 接收重开后 200" ((StatusOf 'http://127.0.0.1:8443/u') -eq 200)

$downloadsDir = Join-Path $env:USERPROFILE "Downloads\QuickDrop"
if (Test-Path $downloadsDir) {
    Get-ChildItem $downloadsDir -Filter "uploadtest*" -ErrorAction SilentlyContinue | Remove-Item -Force
}
& curl.exe -s -X POST -F "file=@$testPng;filename=uploadtest.png" http://127.0.0.1:8443/upload | Out-Null
Start-Sleep -Milliseconds 500
Check "uploadtest.png 已落 ~\Downloads\QuickDrop\" (Test-Path (Join-Path $downloadsDir "uploadtest.png"))

# 清理 daemon
Stop-Process -Id $d.Id -Force -ErrorAction SilentlyContinue
Get-ChildItem $downloadsDir -Filter "uploadtest*" -ErrorAction SilentlyContinue | Remove-Item -Force
Start-Sleep -Seconds 1

# Test 4: send X 模式 + 用 recv IPC 切到接收 → /u 200, /d 仍 200 (发送/接收可共存)
Write-Host ""
Write-Host "=== Test 4: send 模式 + IPC recv on 共存, 发送和接收都可用 ===" -ForegroundColor Cyan
$d = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3
Check "send 启动后 / 200" ((StatusOf 'http://127.0.0.1:8443/') -eq 200)
Check "send 启动后 /u 默认 404" ((StatusOf 'http://127.0.0.1:8443/u') -eq 404)
Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/internal/receive -Body "on" | Out-Null
Start-Sleep -Milliseconds 300
Check "IPC recv on 后 /u 200" ((StatusOf 'http://127.0.0.1:8443/u') -eq 200)
Check "/ 仍 200 (发送不受影响)" ((StatusOf 'http://127.0.0.1:8443/') -eq 200)
Check "/d 仍 200" ((StatusOf 'http://127.0.0.1:8443/d') -eq 200)

# Test 5: /internal/receive 仅 127.0.0.1 (LAN 应 404)
Write-Host ""
Write-Host "=== Test 5: LAN /internal/receive 应被 requireLocal 拒绝 ===" -ForegroundColor Cyan
$lanIP = ((Get-Content "$env:TEMP\quickdrop.log" -ErrorAction SilentlyContinue) -join "`n" | Select-String -Pattern "http://(\d+\.\d+\.\d+\.\d+):8443" -AllMatches).Matches | ForEach-Object { $_.Groups[1].Value } | Select-Object -First 1
if ($lanIP -and $lanIP -ne "127.0.0.1") {
    $code = StatusOf "http://$lanIP`:8443/internal/receive"
    Check "LAN /internal/receive 返回 404 (实际 $code)" ($code -eq 404)
} else {
    Write-Host "  SKIP: 无法检测 LAN IP" -ForegroundColor Yellow
}

# Cleanup
Stop-Process -Id $d.Id -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1

Write-Host ""
if ($fail -eq 0) {
    Write-Host "ALL PASS" -ForegroundColor Green
    exit 0
} else {
    Write-Host "FAILED: $fail check(s)" -ForegroundColor Red
    exit 1
}
