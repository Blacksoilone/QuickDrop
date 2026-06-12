# Phase 2.5e 验收: pending dashboard + 托盘红点 fallback.
#
# 跑法: .\test\test-pending.ps1
# 退出码: 0 PASS, 1 FAIL
#
# 测试内容:
#   1. /p 路由可达 (Vue 骨架 + 引用 p-*.js chunk)
#   2. /api/pending 路由可达, 返回 JSON 数组
#   3. /peer/incoming 触发后 /api/pending 立即有 1 条
#   4. /internal/peer-decide reject 后, pending 该项 state=rejected
#   5. /peer/incoming 多条 → PendingCount 增长 (从 server 视角看)
#
# 托盘红点切换 (tray.SetPendingCount) 是 GUI, 无法自动验证, 要肉眼.

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $root "quickdrop.exe"
$testPng = Join-Path $root "test.png"

if (-not (Test-Path $exe)) { Write-Host "FAIL: $exe 不存在" -ForegroundColor Red; exit 1 }

Get-Process quickdrop -ErrorAction SilentlyContinue | ForEach-Object {
    Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
}
Start-Sleep -Seconds 1

$fail = 0
function Check($name, $cond) {
    if ($cond) { Write-Host "  PASS: $name" -ForegroundColor Green }
    else { Write-Host "  FAIL: $name" -ForegroundColor Red; $script:fail++ }
}

Remove-Item -LiteralPath "$env:TEMP\quickdrop.log" -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "=== 起 daemon ===" -ForegroundColor Cyan
$d = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3
Check "daemon 存活" (-not $d.HasExited)
if ($d.HasExited) { exit 1 }

Write-Host ""
Write-Host "=== /p 路由可达, 返回 Vue 骨架 ===" -ForegroundColor Cyan
$r = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/p
Check "/p 状态 200" ($r.StatusCode -eq 200)
Check "/p 含 #app" ($r.Content -match 'id="app"')
Check "/p 引用 /assets/p-*.js" ($r.Content -match '/assets/p-')

Write-Host ""
Write-Host "=== /api/pending 初始为空 ===" -ForegroundColor Cyan
$pending0 = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/api/pending | Select-Object -ExpandProperty Content | ConvertFrom-Json
$count0 = @($pending0).Count
Check "初始 pending count=0 (实际 $count0)" ($count0 -eq 0)

# 测试用直接 POST /peer/incoming 模拟邀请, 跳过 /internal/peer-send → toast 链路,
# 否则 daemon 自己弹的 toast 异步触发 url-action 进程可能干扰 pending 状态.
function PostIncoming([string]$token) {
    $body = @{
        token = $token
        from = @{ uuid = "00000000000000000000000000000001"; name = "TestSender"; host = ""; ipv4 = "127.0.0.1"; port = 8443 }
        fileName = "test.png"
        fileSize = 1141522
    } | ConvertTo-Json -Compress
    Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/peer/incoming -Body $body -ContentType "application/json" | Out-Null
}

# PS 5.1 ConvertFrom-Json 把 JSON array 解析成 PSCustomObject[], 单元素时则解成
# 单个 PSCustomObject (非数组). 用 `,@(...)` 一律 wrap 成数组确保 .Count 工作.
function PendingItems() {
    $raw = (Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/api/pending).Content
    $parsed = $raw | ConvertFrom-Json
    if ($null -eq $parsed) { return ,@() }
    if ($parsed -is [array]) { return ,$parsed }
    return ,@($parsed)
}

$token1 = "1111111111111111111111111111aaaa"
$token2 = "2222222222222222222222222222bbbb"

Write-Host ""
Write-Host "=== POST /peer/incoming token1, pending count=1 ===" -ForegroundColor Cyan
PostIncoming $token1
Start-Sleep -Milliseconds 300
$pending1 = PendingItems
Check "incoming 后 pending count=1 (实际 $($pending1.Count))" ($pending1.Count -eq 1)
$item1 = $pending1 | Where-Object { $_.token -eq $token1 } | Select-Object -First 1
Check "条目 token 匹配" ($null -ne $item1)
Check "条目 state=pending" ($item1.state -eq "pending")
Check "条目 fileName=test.png" ($item1.fileName -eq "test.png")
Check "条目 fileSize > 0" ($item1.fileSize -gt 0)

Write-Host ""
Write-Host "=== POST 第二条 token2, pending count=2 ===" -ForegroundColor Cyan
PostIncoming $token2
Start-Sleep -Milliseconds 300
$pending2 = PendingItems
Check "pending count=2 (实际 $($pending2.Count))" ($pending2.Count -eq 2)

Write-Host ""
Write-Host "=== Reject token1, state 变 rejected, count 仍 2 (rejected 留 1h GC) ===" -ForegroundColor Cyan
$rejBody = @{ token = $token1; decision = "reject" } | ConvertTo-Json -Compress
Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/internal/peer-decide -Body $rejBody -ContentType "application/json" | Out-Null
Start-Sleep -Milliseconds 300
$pending3 = PendingItems
$reject1 = $pending3 | Where-Object { $_.token -eq $token1 } | Select-Object -First 1
Check "reject 后该条 state=rejected" ($reject1.state -eq "rejected")
$stillPending = $pending3 | Where-Object { $_.state -eq "pending" }
Check "仍 1 条 pending (token2 没决策)" (@($stillPending).Count -eq 1)

# 清理
Stop-Process -Id $d.Id -Force -ErrorAction SilentlyContinue

Write-Host ""
if ($fail -eq 0) {
    Write-Host "ALL PASS" -ForegroundColor Green
    Write-Host "注: 托盘红点图标切换 + tooltip 文字变化 + 菜单项显隐是 GUI 行为, 需肉眼验证" -ForegroundColor Yellow
    exit 0
} else {
    Write-Host "FAILED: $fail check(s)" -ForegroundColor Red
    exit 1
}
