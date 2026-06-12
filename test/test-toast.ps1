# Phase 2.5c 验收: Toast 通知 + quickdrop:// URL scheme.
#
# 跑法: .\test\test-toast.ps1
# 退出码: 0 PASS, 1 FAIL
#
# 测试内容:
#   1. install 同时注册右键菜单 + URL scheme (HKCU\Software\Classes\quickdrop)
#   2. URL scheme command 含 "url-action %1"
#   3. url-action 子命令 - 调对运行中 daemon /internal/peer-decide
#   4. uninstall 清干净 URL scheme 子键
#
# Toast 实弹无法自动验证 (要看屏幕) - 留给手动. 但调用链已通.

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $root "quickdrop.exe"
$testPng = Join-Path $root "test.png"

if (-not (Test-Path $exe)) {
    Write-Host "FAIL: $exe 不存在" -ForegroundColor Red
    exit 1
}

Get-Process quickdrop -ErrorAction SilentlyContinue | ForEach-Object {
    Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
}
Start-Sleep -Seconds 1

$fail = 0
function Check($name, $cond) {
    if ($cond) { Write-Host "  PASS: $name" -ForegroundColor Green }
    else { Write-Host "  FAIL: $name" -ForegroundColor Red; $script:fail++ }
}

function RunExe([string[]]$argv, [int]$timeoutMs = 5000) {
    $p = Start-Process -FilePath $exe -ArgumentList $argv -PassThru -WindowStyle Hidden
    if (-not $p.WaitForExit($timeoutMs)) {
        Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
        return -1
    }
    return $p.ExitCode
}

$schemePath = "HKCU:\Software\Classes\quickdrop"
$schemeCmd  = "$schemePath\shell\open\command"

# Backup install 状态
$hadCtx = Test-Path -LiteralPath "HKCU:\Software\Classes\*\shell\QuickDrop"
$hadScheme = Test-Path -LiteralPath $schemePath
if ($hadCtx -or $hadScheme) {
    Write-Host "WARN: 测试前注册表已有 QuickDrop, 备份后恢复" -ForegroundColor Yellow
    RunExe @("uninstall","-q") | Out-Null
}

Write-Host ""
Write-Host "=== install 同时写右键菜单 + URL scheme ===" -ForegroundColor Cyan
$rc = RunExe @("install","-q")
Check "install 退出码 0" ($rc -eq 0)
Check "右键菜单 HKCU\...\QuickDrop 主键存在" (Test-Path -LiteralPath "HKCU:\Software\Classes\*\shell\QuickDrop")
Check "URL scheme HKCU\...\quickdrop 主键存在" (Test-Path -LiteralPath $schemePath)
Check "URL scheme command 子键存在" (Test-Path -LiteralPath $schemeCmd)

if (Test-Path -LiteralPath $schemePath) {
    $proto = (Get-ItemProperty -LiteralPath $schemePath -Name "URL Protocol" -ErrorAction SilentlyContinue)."URL Protocol"
    Check '"URL Protocol" 标志存在 (空字符串)' ($null -ne $proto)
    $desc = (Get-ItemProperty -LiteralPath $schemePath -Name "(default)" -ErrorAction SilentlyContinue)."(default)"
    Check "scheme 描述含 'QuickDrop Protocol' (实际 '$desc')" ($desc -match "QuickDrop")
}

if (Test-Path -LiteralPath $schemeCmd) {
    $cmd = (Get-ItemProperty -LiteralPath $schemeCmd -Name "(default)" -ErrorAction SilentlyContinue)."(default)"
    Check "command 含 exe 路径" ($cmd -match [regex]::Escape("$exe"))
    Check "command 含 url-action 子命令" ($cmd -match '" url-action "')
    Check "command 以 %1 结尾" ($cmd -match '%1"\s*$')
}

Write-Host ""
Write-Host "=== url-action 子命令解析 + 调 daemon IPC ===" -ForegroundColor Cyan
# 起一个 daemon 接收 IPC 调用
$d = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3
Check "daemon 启动" (-not $d.HasExited)

# 制造一条假的 pending: 让 daemon 自己 POST /peer/incoming (绕开 daemon 找不到对端的问题)
$incoming = @{
    token = "fake1234567890abcdef1234567890abcdef"
    from = @{ uuid = "00000000000000000000000000000000"; name = "TestSender"; host = ""; ipv4 = "127.0.0.1"; port = 8443 }
    fileName = "fake.bin"
    fileSize = 100
} | ConvertTo-Json -Compress
Invoke-WebRequest -UseBasicParsing -Method Post -Uri http://127.0.0.1:8443/peer/incoming -Body $incoming -ContentType "application/json" | Out-Null

# 验证 pending 进了
$pendingBefore = @(Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/api/pending | ConvertFrom-Json)
Check "pending 队列含 fake token" ($null -ne ($pendingBefore | Where-Object { $_.token -eq "fake1234567890abcdef1234567890abcdef" }))

# 跑 url-action quickdrop://reject?token=fake1234... → 应触发 IPC reject
$rc = RunExe @("url-action","quickdrop://reject?token=fake1234567890abcdef1234567890abcdef")
Check "url-action reject 退出码 0" ($rc -eq 0)
Start-Sleep -Milliseconds 300

$pendingAfter = @(Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/api/pending | ConvertFrom-Json)
$item = $pendingAfter | Where-Object { $_.token -eq "fake1234567890abcdef1234567890abcdef" } | Select-Object -First 1
Check "pending 该 token state 改为 rejected" ($item.state -eq "rejected")

Stop-Process -Id $d.Id -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1

Write-Host ""
Write-Host "=== uninstall 清掉 URL scheme + 右键菜单 ===" -ForegroundColor Cyan
$rc = RunExe @("uninstall","-q")
Check "uninstall 退出码 0" ($rc -eq 0)
Check "右键菜单主键不存在" (-not (Test-Path -LiteralPath "HKCU:\Software\Classes\*\shell\QuickDrop"))
Check "URL scheme 主键不存在" (-not (Test-Path -LiteralPath $schemePath))
Check "URL scheme command 子键不存在" (-not (Test-Path -LiteralPath $schemeCmd))

# 还原备份: 如果脚本前已 install 过, 重新 install
if ($hadCtx -or $hadScheme) {
    Write-Host ""
    Write-Host "Restoring previous install state..." -ForegroundColor Yellow
    RunExe @("install","-q") | Out-Null
}

Write-Host ""
if ($fail -eq 0) {
    Write-Host "ALL PASS" -ForegroundColor Green
    Write-Host "注: 实际 toast 弹出无法自动验证, 需肉眼看. 调用链 (daemon → notify.Incoming → toast.Push) 已通过编译." -ForegroundColor Yellow
    exit 0
} else {
    Write-Host "FAILED: $fail check(s)" -ForegroundColor Red
    exit 1
}
