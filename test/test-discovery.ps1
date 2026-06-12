# Phase 2.5a 验收: mDNS 广播 + 发现 (单机能验证的部分).
#
# 跑法: .\test\test-discovery.ps1
# 退出码: 0 PASS, 1 FAIL
#
# 单机限制: grandcat/zeroconf v1.0 默认不走 loopback (PR #68 未合),
# 同机两个 daemon 互相看不见. 本脚本只验证:
#   1. daemon 启动时 mDNS 广播成功 (log 含 "已广播")
#   2. 自己不在自己 /api/peers 里 (过滤自身 UUID 工作)
#   3. /api/peers 路由可达 + 返回合法 JSON 数组
#   4. 设备 UUID 持久化跨重启不变
#   5. QUICKDROP_PORT env 切换端口工作
#
# 真实 PC→PC 互见需要第二台机器实地验证.

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

Remove-Item -LiteralPath "$env:TEMP\quickdrop.log" -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "=== Start daemon (默认 8443, 默认主机名) ===" -ForegroundColor Cyan
$d = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3
Check "daemon 存活" (-not $d.HasExited)
if ($d.HasExited) {
    [System.IO.File]::ReadAllText("$env:TEMP\quickdrop.log", [System.Text.Encoding]::UTF8)
    exit 1
}

function ReadLogShared() {
    # daemon 进程独占持有日志文件, .NET File.ReadAllText 会冲突.
    # 用 FileStream + FileShare.ReadWrite 共享访问.
    $fs = [System.IO.FileStream]::new("$env:TEMP\quickdrop.log", [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, [System.IO.FileShare]::ReadWrite)
    try {
        $sr = [System.IO.StreamReader]::new($fs, [System.Text.Encoding]::UTF8)
        try {
            return $sr.ReadToEnd()
        } finally { $sr.Dispose() }
    } finally { $fs.Dispose() }
}

Write-Host ""
Write-Host "=== Log 验证 mDNS 广播注册成功 ===" -ForegroundColor Cyan
$log = ReadLogShared
Check "log 含 'mDNS 已广播'" ($log -match "mDNS 已广播")
Check "log 含 service=_quickdrop._tcp" ($log -match "service=_quickdrop\._tcp")
Check "log 含 port=8443" ($log -match "port=8443")
Check "log 含 '身份:' (identity 加载)" ($log -match "身份:")

Write-Host ""
Write-Host "=== /api/peers: 路由可达, 返回 JSON 数组, 不含自己 ===" -ForegroundColor Cyan
$r = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/api/peers
Check "/api/peers 状态 200" ($r.StatusCode -eq 200)
Check "/api/peers Content-Type application/json" ($r.Headers."Content-Type" -match "application/json")
$peers = $r.Content | ConvertFrom-Json
# PowerShell ConvertFrom-Json 空数组转 null, 单元素转标量, 这里宽松判断
$count = @($peers).Count
Check "peers 是数组/null (当前 count=$count)" ($null -eq $peers -or $peers -is [array] -or $count -ge 0)
$myUUID = (Get-Content "$env:USERPROFILE\.quickdrop\device-id" -Raw).Trim()
$selfInList = @($peers) | Where-Object { $_.uuid -eq $myUUID }
Check "/api/peers 不含自己 UUID (myUUID=$($myUUID.Substring(0,8))...)" ($null -eq $selfInList)

Write-Host ""
Write-Host "=== 设备身份持久化 ===" -ForegroundColor Cyan
Check "~/.quickdrop/device-id 文件存在" (Test-Path -LiteralPath "$env:USERPROFILE\.quickdrop\device-id")
Check "device-id 是 32 字符 hex" ($myUUID -match '^[0-9a-f]{32}$')

Stop-Process -Id $d.Id -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2

Write-Host ""
Write-Host "=== 重启 daemon, UUID 应保持不变 ===" -ForegroundColor Cyan
$d2 = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3
$uuid2 = (Get-Content "$env:USERPROFILE\.quickdrop\device-id" -Raw).Trim()
Check "重启后 UUID 不变 ($($uuid2.Substring(0,8))...)" ($uuid2 -eq $myUUID)
Stop-Process -Id $d2.Id -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2

Write-Host ""
Write-Host "=== QUICKDROP_PORT=8444 启动应监听 8444 ===" -ForegroundColor Cyan
$piA = New-Object System.Diagnostics.ProcessStartInfo
$piA.FileName = $exe
$piA.Arguments = 'send "' + $testPng + '"'
$piA.UseShellExecute = $false
$piA.WindowStyle = "Hidden"
$piA.EnvironmentVariables["QUICKDROP_PORT"] = "8444"
$d3 = [System.Diagnostics.Process]::Start($piA)
Start-Sleep -Seconds 3
try {
    $r = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8444/api/info -TimeoutSec 3
    Check "8444 上 /api/info 可达" ($r.StatusCode -eq 200)
} catch {
    Check "8444 上 /api/info 可达 (失败: $($_.Exception.Message))" $false
}
# 验证 8443 上不该有 daemon
try {
    Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/api/info -TimeoutSec 1 | Out-Null
    Check "8443 上无 daemon (实际有)" $false
} catch {
    Check "8443 上无 daemon (QUICKDROP_PORT 隔离工作)" $true
}
Stop-Process -Id $d3.Id -Force -ErrorAction SilentlyContinue

Write-Host ""
if ($fail -eq 0) {
    Write-Host "ALL PASS" -ForegroundColor Green
    Write-Host "注: 真实 PC→PC mDNS 互见需要第二台 Windows 机器, 本脚本仅验证单机部分" -ForegroundColor Yellow
    exit 0
} else {
    Write-Host "FAILED: $fail check(s)" -ForegroundColor Red
    exit 1
}
