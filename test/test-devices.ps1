# Phase 2.6 验收: 设备信任表 + auto-accept / blocked 分支.
#
# 跑法: .\test\test-devices.ps1
# 退出码: 0 PASS, 1 FAIL
#
# 测试内容:
#   1. /v 路由返回 Vue 骨架
#   2. /api/devices 路由可达
#   3. UpsertSeen: 收到 incoming 后设备入表
#   4. /internal/device-trust 改 trust → 持久化
#   5. trusted 设备: incoming 立即 accept 文件落盘 (不要 toast 等)
#   6. blocked 设备: incoming 不入 pending
#   7. ~/.quickdrop/devices.json 真实写入 + 重启不丢

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $root "quickdrop.exe"
$testPng = Join-Path $root "test.png"

if (-not (Test-Path $exe)) { Write-Host "FAIL: $exe 不存在" -ForegroundColor Red; exit 1 }

Get-Process quickdrop -ErrorAction SilentlyContinue | ForEach-Object {
    Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
}
Start-Sleep -Seconds 1

# 备份用户的 devices.json (脚本会改它)
$devicesPath = Join-Path $env:USERPROFILE ".quickdrop\devices.json"
$backup = $null
if (Test-Path -LiteralPath $devicesPath) {
    $backup = [System.IO.File]::ReadAllText($devicesPath)
    Remove-Item -LiteralPath $devicesPath -Force
}

$fail = 0
function Check($name, $cond) {
    if ($cond) { Write-Host "  PASS: $name" -ForegroundColor Green }
    else { Write-Host "  FAIL: $name" -ForegroundColor Red; $script:fail++ }
}

function PendingItems() {
    $raw = (Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/api/pending).Content
    $parsed = $raw | ConvertFrom-Json
    if ($null -eq $parsed) { return ,@() }
    if ($parsed -is [array]) { return ,$parsed }
    return ,@($parsed)
}

function DeviceItems() {
    $raw = (Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/api/devices).Content
    $parsed = $raw | ConvertFrom-Json
    if ($null -eq $parsed) { return ,@() }
    if ($parsed -is [array]) { return ,$parsed }
    return ,@($parsed)
}

# 起 daemon (用 send + test.png; Alice 这个 daemon 既是发方又是收方)
# 同时是 sender 让 Pull 有源
$d = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3
Check "daemon 存活" (-not $d.HasExited)
if ($d.HasExited) { exit 1 }

Write-Host ""
Write-Host "=== /v 路由可达 ===" -ForegroundColor Cyan
$r = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/v
Check "/v 状态 200" ($r.StatusCode -eq 200)
Check "/v 含 #app" ($r.Content -match 'id="app"')
Check "/v 引用 /assets/v-*.js" ($r.Content -match '/assets/v-')

Write-Host ""
Write-Host "=== /api/devices 初始为空 ===" -ForegroundColor Cyan
$dev0 = DeviceItems
Check "初始 devices count=0" ($dev0.Count -eq 0)

# 测试用 UUID (固定方便跨调用引用)
$uuidA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
$uuidB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

Write-Host ""
Write-Host "=== 模拟 incoming → UpsertSeen 入表 (trust=ask) ===" -ForegroundColor Cyan
$body = @{
    token = "1111111111111111111111111111ABCD"
    from = @{ uuid = $uuidA; name = "DeviceA"; host = ""; ipv4 = "127.0.0.1"; port = 8443 }
    fileName = "test.png"; fileSize = 1141522
} | ConvertTo-Json -Compress
Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/peer/incoming -Body $body -ContentType "application/json" | Out-Null
Start-Sleep -Milliseconds 300
$dev1 = DeviceItems
Check "incoming 后 devices count=1" ($dev1.Count -eq 1)
$itemA = $dev1 | Where-Object { $_.uuid -eq $uuidA }
Check "DeviceA 入表" ($null -ne $itemA)
Check "DeviceA name=DeviceA" ($itemA.name -eq "DeviceA")
Check "DeviceA trust=ask (默认)" ($itemA.trust -eq "ask")
# 该 incoming 仍应入 pending (trust=ask)
$pendingAsk = PendingItems
$pAsk = $pendingAsk | Where-Object { $_.token -eq "1111111111111111111111111111ABCD" }
Check "ask trust → incoming 进 pending" ($null -ne $pAsk)
Check "pending 项 trust 字段=ask" ($pAsk.trust -eq "ask")

Write-Host ""
Write-Host "=== /internal/device-trust 设 DeviceA = trusted ===" -ForegroundColor Cyan
$tBody = @{ uuid = $uuidA; name = "DeviceA"; trust = "trusted" } | ConvertTo-Json -Compress
$r = Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/internal/device-trust -Body $tBody -ContentType "application/json"
Check "device-trust 返回 200" ($r.StatusCode -eq 200)
$dev2 = DeviceItems
$itemA2 = $dev2 | Where-Object { $_.uuid -eq $uuidA }
Check "DeviceA trust 已变 trusted" ($itemA2.trust -eq "trusted")
Check "devices.json 真实写入" (Test-Path -LiteralPath $devicesPath)

Write-Host ""
Write-Host "=== Trusted 设备的 incoming 应该立刻 accept + 触发 Pull ===" -ForegroundColor Cyan
# 清掉 downloads/QuickDrop/test.png 以观察 Pull 落盘
$dlDir = Join-Path $env:USERPROFILE "Downloads\QuickDrop"
if (Test-Path -LiteralPath (Join-Path $dlDir "test.png")) {
    Remove-Item -LiteralPath (Join-Path $dlDir "test.png") -Force
}
# 需要有真 outgoing 才能 Pull 成功; 走 /internal/peer-send 让 daemon 创建 outgoing 再 POST 自己的 /peer/incoming
# 但 peer-send 默认 from.uuid 是 daemon 自身 UUID, 不是 uuidA → trusted 判定 NG
# 替代方案: 直接 POST /peer/incoming 模拟来源是 DeviceA (trusted), 但 Pull 时找不到 outgoing → 失败
# 这里只验证 "trust 分支" 工作 (pending 立刻变 accepted), 不验证文件落盘
$token2 = "2222222222222222222222222222ABCD"
$body2 = @{
    token = $token2
    from = @{ uuid = $uuidA; name = "DeviceA"; host = ""; ipv4 = "127.0.0.1"; port = 8443 }
    fileName = "test.png"; fileSize = 1141522
} | ConvertTo-Json -Compress
Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/peer/incoming -Body $body2 -ContentType "application/json" | Out-Null
Start-Sleep -Milliseconds 500
$pendingTrust = PendingItems
$pTrust = $pendingTrust | Where-Object { $_.token -eq $token2 }
Check "trusted 设备 incoming 入了 pending" ($null -ne $pTrust)
Check "trusted 设备 incoming 立即 state=accepted" ($pTrust.state -eq "accepted")
Check "trusted 设备 pending 项 trust=trusted" ($pTrust.trust -eq "trusted")

Write-Host ""
Write-Host "=== 设 DeviceB = blocked, 它的 incoming 不入 pending ===" -ForegroundColor Cyan
$tBody2 = @{ uuid = $uuidB; name = "DeviceB"; trust = "blocked" } | ConvertTo-Json -Compress
Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/internal/device-trust -Body $tBody2 -ContentType "application/json" | Out-Null
$token3 = "3333333333333333333333333333ABCD"
$body3 = @{
    token = $token3
    from = @{ uuid = $uuidB; name = "DeviceB"; host = ""; ipv4 = "127.0.0.1"; port = 8443 }
    fileName = "evil.exe"; fileSize = 666
} | ConvertTo-Json -Compress
Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/peer/incoming -Body $body3 -ContentType "application/json" | Out-Null
Start-Sleep -Milliseconds 300
$pendingAll = PendingItems
$pBlocked = $pendingAll | Where-Object { $_.token -eq $token3 }
Check "blocked 设备 incoming 不入 pending" ($null -eq $pBlocked)

Write-Host ""
Write-Host "=== 重启 daemon, devices.json 状态保持 ===" -ForegroundColor Cyan
Stop-Process -Id $d.Id -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2
$d2 = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3
$devReboot = DeviceItems
Check "重启后 devices count=2" ($devReboot.Count -eq 2)
$itemA3 = $devReboot | Where-Object { $_.uuid -eq $uuidA }
$itemB3 = $devReboot | Where-Object { $_.uuid -eq $uuidB }
Check "重启后 DeviceA trust 仍 trusted" ($itemA3.trust -eq "trusted")
Check "重启后 DeviceB trust 仍 blocked" ($itemB3.trust -eq "blocked")

Stop-Process -Id $d2.Id -Force -ErrorAction SilentlyContinue
Get-ChildItem $dlDir -Filter "test.png" -ErrorAction SilentlyContinue | Remove-Item -Force

# 还原备份的 devices.json
Remove-Item -LiteralPath $devicesPath -Force -ErrorAction SilentlyContinue
if ($backup) {
    [System.IO.File]::WriteAllText($devicesPath, $backup)
    Write-Host "  (已还原原 devices.json)" -ForegroundColor DarkGray
}

Write-Host ""
if ($fail -eq 0) {
    Write-Host "ALL PASS" -ForegroundColor Green
    exit 0
} else {
    Write-Host "FAILED: $fail check(s)" -ForegroundColor Red
    exit 1
}
