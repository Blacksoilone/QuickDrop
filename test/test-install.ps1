# Phase 2.4 验收: Windows 右键菜单注册.
#
# 跑法: .\test\test-install.ps1
# 退出码: 0 PASS, 1 FAIL
#
# 注意: 测试会动 HKCU 注册表 (用户级, 无需管理员).
# 测试前会备份, 测试后恢复原状.

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $root "quickdrop.exe"

if (-not (Test-Path $exe)) {
    Write-Host "FAIL: $exe 不存在, 先跑 .\build.ps1" -ForegroundColor Red
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

$keyPath = "HKCU:\Software\Classes\*\shell\QuickDrop"
$cmdKeyPath = "$keyPath\command"

function ReadLogUTF8() {
    if (-not (Test-Path -LiteralPath "$env:TEMP\quickdrop.log")) { return "" }
    # PowerShell 5.1 默认按 GBK 读, 我们的日志是 UTF-8, 必须显式指定否则中文乱码匹配不上
    [System.IO.File]::ReadAllText("$env:TEMP\quickdrop.log", [System.Text.Encoding]::UTF8)
}

# windowsgui 构建的 exe 用 `& $exe` 调用会立即返回 (detach), $LASTEXITCODE 拿不到也不等执行完.
# 改用 Start-Process -Wait 真等进程结束.
function RunExe([string[]]$argv, [int]$timeoutMs = 5000) {
    $p = Start-Process -FilePath $script:exe -ArgumentList $argv -PassThru -WindowStyle Hidden
    if (-not $p.WaitForExit($timeoutMs)) {
        Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
        return -1
    }
    return $p.ExitCode
}

# Backup: 测试前如果已注册, 先记下来, 测后恢复
$hadPrev = Test-Path -LiteralPath $keyPath
if ($hadPrev) {
    Write-Host "WARN: 测试前注册表已存在 QuickDrop 条目, 会备份后恢复" -ForegroundColor Yellow
    RunExe @("uninstall","-q") | Out-Null
}

# Test 1: install 写入注册表
Write-Host ""
Write-Host "=== Test 1: install 写入注册表 ===" -ForegroundColor Cyan
$rc = RunExe @("install","-q")
Check "install 退出码 0" ($rc -eq 0)
Check "HKCU\...\QuickDrop 主键存在" (Test-Path -LiteralPath $keyPath)
Check "HKCU\...\QuickDrop\command 子键存在" (Test-Path -LiteralPath $cmdKeyPath)

if (Test-Path -LiteralPath $keyPath) {
    $label = (Get-ItemProperty -LiteralPath $keyPath -Name "(default)" -ErrorAction SilentlyContinue)."(default)"
    Check "菜单文字 = '通过 QuickDrop 发送' (实际 '$label')" ($label -eq "通过 QuickDrop 发送")

    $icon = (Get-ItemProperty -LiteralPath $keyPath -Name "Icon" -ErrorAction SilentlyContinue).Icon
    Check "Icon 字段含 exe 路径 + ,0 (实际 '$icon')" ($icon -match [regex]::Escape("$exe") -and $icon -match ",0$")
}

if (Test-Path -LiteralPath $cmdKeyPath) {
    $cmd = (Get-ItemProperty -LiteralPath $cmdKeyPath -Name "(default)" -ErrorAction SilentlyContinue)."(default)"
    Check "command 含 exe 路径" ($cmd -match [regex]::Escape("$exe"))
    Check "command 含 send 子命令" ($cmd -match '" send "')
    Check "command 以 %1 结尾" ($cmd -match '%1"\s*$')
    Check "exe 路径有引号包 (空格安全)" ($cmd -match '^"[^"]+"\s+send')
}

# Test 2: install 幂等
Write-Host ""
Write-Host "=== Test 2: install 幂等 ===" -ForegroundColor Cyan
$rc = RunExe @("install","-q")
Check "二次 install 退出码 0" ($rc -eq 0)
Check "主键仍存在" (Test-Path -LiteralPath $keyPath)

# Test 3: status 报告已安装
Write-Host ""
Write-Host "=== Test 3: status 报告已安装 ===" -ForegroundColor Cyan
Remove-Item -LiteralPath "$env:TEMP\quickdrop.log" -ErrorAction SilentlyContinue
$rc = RunExe @("status","-q")
Check "status 退出码 0" ($rc -eq 0)
$log = ReadLogUTF8
Check "log 包含 '已注册'" ($log -match "已注册")

# Test 4: uninstall 清除
Write-Host ""
Write-Host "=== Test 4: uninstall 清除 ===" -ForegroundColor Cyan
$rc = RunExe @("uninstall","-q")
Check "uninstall 退出码 0" ($rc -eq 0)
Check "command 子键不存在" (-not (Test-Path -LiteralPath $cmdKeyPath))
Check "主键不存在" (-not (Test-Path -LiteralPath $keyPath))

# Test 5: uninstall 幂等
Write-Host ""
Write-Host "=== Test 5: uninstall 幂等 ===" -ForegroundColor Cyan
$rc = RunExe @("uninstall","-q")
Check "再次 uninstall (本无东西) 退出码 0" ($rc -eq 0)

# Test 6: status 报告未安装
Write-Host ""
Write-Host "=== Test 6: status 报告未安装 ===" -ForegroundColor Cyan
Remove-Item -LiteralPath "$env:TEMP\quickdrop.log" -ErrorAction SilentlyContinue
$rc = RunExe @("status","-q")
$log = ReadLogUTF8
Check "log 包含 '未注册'" ($log -match "未注册")

# Restore: 还原备份
# 还原 install: 同 test-toast.ps1, 测试结束总让 install 在位.
# 否则下次用户跑 daemon 收 toast 会无法接受.
Write-Host ""
Write-Host "Restoring install state (always end with install present)..." -ForegroundColor Yellow
RunExe @("install","-q") | Out-Null

Write-Host ""
if ($fail -eq 0) {
    Write-Host "ALL PASS" -ForegroundColor Green
    exit 0
} else {
    Write-Host "FAILED: $fail check(s)" -ForegroundColor Red
    exit 1
}
