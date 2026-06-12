# 手动验证 toast 接收全流程的便利脚本.
#
# 跑法: .\test\manual-toast-demo.ps1
#
# 不是自动化测试, 是给你"看 toast 弹出 + 点接受 + 文件落盘"的体验脚本.
# 全程不杀 daemon, 不清 Downloads, 跑完后东西都还在.

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $root "quickdrop.exe"
$testPng = Join-Path $root "test.png"

if (-not (Test-Path $exe)) { Write-Host "FAIL: $exe 不存在, 先跑 .\build.ps1" -ForegroundColor Red; exit 1 }

# 自检 install 是否在位 (URL scheme 没装的话 toast 按钮会失效)
$schemeOK = Test-Path -LiteralPath "HKCU:\Software\Classes\quickdrop\shell\open\command"
if (-not $schemeOK) {
    Write-Host "URL scheme 未注册, 自动 install 修复..." -ForegroundColor Yellow
    & $exe install -q
    Start-Sleep -Seconds 1
}

# 起 daemon (默认 8443) 如果没在跑
$d = Get-Process quickdrop -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowTitle -ne "QuickDrop" } | Select-Object -First 1
if (-not $d) {
    Write-Host "起 daemon..." -ForegroundColor Yellow
    Start-Process -FilePath $exe -ArgumentList "send",$testPng -WindowStyle Hidden | Out-Null
    Start-Sleep -Seconds 3
}

# 模拟一条来自"模拟对端"的真实 incoming
# 注意: token 是真随机的, 而且我们让 daemon 先创建 outgoing (走 peer-send) 再发给自己,
# 这样 url-action accept 时 daemon 那边的 outgoing 真存在, Pull 才能成
$absPath = (Resolve-Path $testPng).Path
$body = '{"toIPv4":"127.0.0.1","toPort":8443,"filePath":"' + $absPath.Replace('\','\\') + '"}'
$r = Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/internal/peer-send -Body $body -ContentType "application/json"
$token = ($r.Content | ConvertFrom-Json).token

Write-Host ""
Write-Host "===========================================" -ForegroundColor Green
Write-Host "  Toast 应该已弹出在屏幕右下角" -ForegroundColor Green
Write-Host "===========================================" -ForegroundColor Green
Write-Host ""
Write-Host "接收方 daemon 把这个 token 当 incoming 入了 pending:" -ForegroundColor Cyan
Write-Host "  token = $token"
Write-Host ""
Write-Host "现在请你看右下角:" -ForegroundColor Cyan
Write-Host "  - Toast 标题: '<本机名> 想发文件给你'"
Write-Host "  - Toast 正文: 'test.png (1.1 MiB)'"
Write-Host "  - 两个按钮: [接受] [拒绝]"
Write-Host ""
Write-Host "如果 toast 没弹: 看 Action Center (Win+N) 找 QuickDrop 的通知"
Write-Host ""
Write-Host "点 [接受] 后, ~/Downloads/QuickDrop/test.png 应该被覆盖更新." -ForegroundColor Cyan
Write-Host "点 [拒绝] 啥都不发生."
Write-Host ""
Write-Host "若 5 秒内你不点, 这个脚本会列出 Downloads 状态后退出, daemon 不动." -ForegroundColor DarkGray
Start-Sleep -Seconds 8

Write-Host ""
Write-Host "=== 当前 ~/Downloads/QuickDrop/ ===" -ForegroundColor Cyan
$dlDir = Join-Path $env:USERPROFILE "Downloads\QuickDrop"
if (Test-Path -LiteralPath $dlDir) {
    Get-ChildItem -LiteralPath $dlDir -Force | Format-Table Name, Length, LastWriteTime
}

Write-Host ""
Write-Host "=== /api/pending 当前状态 ===" -ForegroundColor Cyan
$p = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/api/pending | Select-Object -ExpandProperty Content | ConvertFrom-Json
@($p) | Where-Object { $_.token -eq $token } | Format-Table state, fileName, fileSize, @{N="from"; E={$_.from.name}}

Write-Host ""
Write-Host "daemon 仍在跑 (没杀), 你可以继续用. 想关 → 托盘图标右键 → 退出, 或 Stop-Process -Name quickdrop" -ForegroundColor DarkGray
