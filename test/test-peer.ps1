# Phase 2.5b 验收: PC↔PC IPC 协议 (单机双 daemon 模拟).
#
# 跑法: .\test\test-peer.ps1
# 退出码: 0 PASS, 1 FAIL
#
# 单机限制: mDNS 不工作, 用 /internal/peer-send 的直连旁路 (toIPv4+toPort) 跳过对端发现.
# 流程:
#   Alice (8443) ── POST /internal/peer-send {toIPv4:127.0.0.1, toPort:8444, filePath}
#                ─→ POST http://127.0.0.1:8444/peer/incoming {token, from, fileName, ...}
#                ─→ Bob 入 pending queue, /api/pending 能看到
#   Bob (8444)   ── POST /internal/peer-decide {token, decision:accept}
#                ─→ GET http://127.0.0.1:8443/peer/file?token=xxx
#                ─→ saveStream 到 ~/Downloads/QuickDrop/

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

function StartDaemonOnPort([int]$port, [string]$name) {
    $pi = New-Object System.Diagnostics.ProcessStartInfo
    $pi.FileName = $exe
    $pi.Arguments = 'send "' + $testPng + '"'
    $pi.UseShellExecute = $false
    $pi.WindowStyle = "Hidden"
    $pi.EnvironmentVariables["QUICKDROP_PORT"] = "$port"
    $pi.EnvironmentVariables["QUICKDROP_DEVICE_NAME"] = $name
    return [System.Diagnostics.Process]::Start($pi)
}

# 清掉 device-id, 让 A 和 B 用不同 UUID
$idFile = Join-Path $env:USERPROFILE ".quickdrop\device-id"
$backupId = $null
if (Test-Path -LiteralPath $idFile) {
    $backupId = (Get-Content -LiteralPath $idFile -Raw).TrimEnd()
}

Write-Host ""
Write-Host "=== Start Alice (8443) ===" -ForegroundColor Cyan
Remove-Item -LiteralPath $idFile -Force -ErrorAction SilentlyContinue
$pA = StartDaemonOnPort 8443 "Alice"
Start-Sleep -Seconds 3
Check "Alice 存活" (-not $pA.HasExited)

# 等 device-id 已写, 删它让 B 用新 UUID
Start-Sleep -Seconds 1
Remove-Item -LiteralPath $idFile -Force -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "=== Start Bob (8444) ===" -ForegroundColor Cyan
$pB = StartDaemonOnPort 8444 "Bob"
Start-Sleep -Seconds 3
Check "Bob 存活" (-not $pB.HasExited)
if ($pA.HasExited -or $pB.HasExited) {
    Write-Host "daemon 没起来, 看日志:" -ForegroundColor Red
    $fs = [System.IO.FileStream]::new("$env:TEMP\quickdrop.log", "Open", "Read", "ReadWrite")
    try { ([System.IO.StreamReader]::new($fs, [System.Text.Encoding]::UTF8)).ReadToEnd() } finally { $fs.Dispose() }
    exit 1
}

# 清空 Bob 的 Downloads/QuickDrop, 准备接收
# 注意: 仅删测试目标 test.png (其他用户文件不动)
$dlDir = Join-Path $env:USERPROFILE "Downloads\QuickDrop"
if (Test-Path -LiteralPath (Join-Path $dlDir "test.png")) {
    Remove-Item -LiteralPath (Join-Path $dlDir "test.png") -Force
}

Write-Host ""
Write-Host "=== Alice 调 /internal/peer-send 通知 Bob ===" -ForegroundColor Cyan
$sendBody = @{
    toIPv4 = "127.0.0.1"
    toPort = 8444
    filePath = $testPng
} | ConvertTo-Json -Compress
$r = Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/internal/peer-send -Body $sendBody -ContentType "application/json"
Check "Alice peer-send 返回 200" ($r.StatusCode -eq 200)
$resp = $r.Content | ConvertFrom-Json
$token = $resp.token
Check "返回 token (32 hex)" ($token -match '^[0-9a-f]{32}$')
Check "返回 to 字段" (-not [string]::IsNullOrEmpty($resp.to))

Write-Host ""
Write-Host "=== Bob /api/pending 应能看到这条 incoming ===" -ForegroundColor Cyan
$pending = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8444/api/pending | Select-Object -ExpandProperty Content | ConvertFrom-Json
$pendingArr = @($pending)
Check "Bob pending 至少 1 条" ($pendingArr.Count -ge 1)
$item = $pendingArr | Where-Object { $_.token -eq $token } | Select-Object -First 1
Check "找到对应 token 的 pending" ($null -ne $item)
if ($item) {
    Check "pending.state = pending" ($item.state -eq "pending")
    Check "pending.fileName = test.png" ($item.fileName -eq "test.png")
    Check "pending.fileSize > 0" ($item.fileSize -gt 0)
    Check "pending.from.name = Alice" ($item.from.name -eq "Alice")
}

Write-Host ""
Write-Host "=== Bob 接受邀请, 异步 Pull 文件 ===" -ForegroundColor Cyan
$decideBody = @{ token = $token; decision = "accept" } | ConvertTo-Json -Compress
$r = Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8444/internal/peer-decide -Body $decideBody -ContentType "application/json"
Check "Bob peer-decide accept 返回 200" ($r.StatusCode -eq 200)
$decResp = $r.Content | ConvertFrom-Json
Check "decResp.decision = accepted" ($decResp.decision -eq "accepted")

# 等 Pull 完成 (test.png 1.1MB 很快, 给 3 秒余量)
Start-Sleep -Seconds 3

Write-Host ""
Write-Host "=== test.png 应已落到 ~/Downloads/QuickDrop/ ===" -ForegroundColor Cyan
$dst = Join-Path $dlDir "test.png"
Check "test.png 存在" (Test-Path -LiteralPath $dst)
if (Test-Path -LiteralPath $dst) {
    $src = Get-Item -LiteralPath $testPng
    $got = Get-Item -LiteralPath $dst
    Check "字节数匹配 ($($src.Length) vs $($got.Length))" ($src.Length -eq $got.Length)
    # MD5 比对
    $srcHash = (Get-FileHash -LiteralPath $testPng -Algorithm MD5).Hash
    $dstHash = (Get-FileHash -LiteralPath $dst -Algorithm MD5).Hash
    Check "MD5 一致 ($srcHash)" ($srcHash -eq $dstHash)
}

Write-Host ""
Write-Host "=== Token 一次性: 同 token 再 Pull 应 404 (Alice MarkDelivered) ===" -ForegroundColor Cyan
try {
    $r = Invoke-WebRequest -UseBasicParsing "http://127.0.0.1:8443/peer/file?token=$token" -TimeoutSec 3
    Check "第二次 Pull 应 404 (实际 $($r.StatusCode))" $false
} catch {
    $code = $_.Exception.Response.StatusCode.value__
    Check "第二次 Pull 返回 404" ($code -eq 404)
}

Write-Host ""
Write-Host "=== /peer/file 无 token 应 404 ===" -ForegroundColor Cyan
try {
    $r = Invoke-WebRequest -UseBasicParsing "http://127.0.0.1:8443/peer/file" -TimeoutSec 3
    Check "无 token 应 404 (实际 $($r.StatusCode))" $false
} catch {
    Check "无 token Pull 返回 404" ($_.Exception.Response.StatusCode.value__ -eq 404)
}

Write-Host ""
Write-Host "=== Reject 流程: Alice 再 send → Bob reject ===" -ForegroundColor Cyan
$r = Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/internal/peer-send -Body $sendBody -ContentType "application/json"
$token2 = ($r.Content | ConvertFrom-Json).token
$rejectBody = @{ token = $token2; decision = "reject" } | ConvertTo-Json -Compress
$r = Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8444/internal/peer-decide -Body $rejectBody -ContentType "application/json"
Check "Reject 返回 200" ($r.StatusCode -eq 200)
$decResp = $r.Content | ConvertFrom-Json
Check "decResp.decision = rejected" ($decResp.decision -eq "rejected")
# 验证 Bob /api/pending 状态变了
$pending2 = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8444/api/pending | Select-Object -ExpandProperty Content | ConvertFrom-Json
$item2 = @($pending2) | Where-Object { $_.token -eq $token2 } | Select-Object -First 1
Check "Bob pending 该 token state=rejected" ($item2.state -eq "rejected")

Write-Host ""
Write-Host "=== url-action 路径 (= toast 接受按钮等价) ===" -ForegroundColor Cyan
# Alice 第三次发, Bob 用 url-action 子命令 accept (模拟 Windows shell 启动 quickdrop://accept?token=xxx)
$r = Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/internal/peer-send -Body $sendBody -ContentType "application/json"
$token3 = ($r.Content | ConvertFrom-Json).token
Get-ChildItem $dlDir -Filter "test.png" -ErrorAction SilentlyContinue | Remove-Item -Force
# 跑 quickdrop url-action quickdrop://accept?token=xxx, 必须设 QUICKDROP_PORT 让它找到 Bob (8444)
$pi = New-Object System.Diagnostics.ProcessStartInfo
$pi.FileName = $exe
$pi.Arguments = 'url-action "quickdrop://accept?token=' + $token3 + '"'
$pi.UseShellExecute = $false
$pi.WindowStyle = "Hidden"
$pi.EnvironmentVariables["QUICKDROP_PORT"] = "8444"
$urlProc = [System.Diagnostics.Process]::Start($pi)
$ok = $urlProc.WaitForExit(5000)
Check "url-action 子命令 5 秒内退出" $ok
Check "url-action 退出码 0" ($urlProc.ExitCode -eq 0)
# Bob 异步 Pull, 等几秒
Start-Sleep -Seconds 3
$dst3 = Join-Path $dlDir "test.png"
Check "url-action accept 后文件落盘" (Test-Path -LiteralPath $dst3)

# 清理: 只删测试目录里"看起来像我们造的"文件, 不要删任何用户可能保留的.
# 我们 test.png 是仓库根的固定测试文件, 名字 "test.png" 撞用户实际接收过的同名文件,
# 所以这里只清 daemon 进程, 不删 Downloads. 用户后续要清自己删.
Stop-Process -Id $pA.Id -Force -ErrorAction SilentlyContinue
Stop-Process -Id $pB.Id -Force -ErrorAction SilentlyContinue

if ($backupId) {
    [System.IO.File]::WriteAllText($idFile, $backupId)
}

Write-Host ""
if ($fail -eq 0) {
    Write-Host "ALL PASS" -ForegroundColor Green
    exit 0
} else {
    Write-Host "FAILED: $fail check(s)" -ForegroundColor Red
    exit 1
}
