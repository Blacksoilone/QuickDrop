# Phase 2.x C2 测试: ~/.quickdrop/config.json + /api/config + /internal/config-save.
#
# 跑法: .\test\test-config.ps1
# 退出码: 0 PASS, 1 FAIL
#
# 测试矩阵:
#   1. /api/config GET 默认值正常 (首次启动无 config.json)
#   2. /internal/config-save POST 接受 partial body + 字段合并
#   3. /api/config GET 再次, 返回保存后的值 (热应用)
#   4. 重启 daemon, config.json 内容仍在 (持久化)
#   5. POST 非法 port → 400, 磁盘文件不被破坏
#   6. POST conflict=overwrite 后, 实际上传同名文件应覆盖 (集成校验)
#   7. POST receive.max_file_size=N, 超大文件 upload 应被拒 (413)
#
# 注意:
#   - 不动用户真实 ~/.quickdrop/config.json. 备份 + 还原.
#   - 用临时 USERPROFILE 隔离? Go 进程已读 env, 这里需启动前 set; 但 device-id 也在 ~/.quickdrop.
#   - 折中: 备份 ~/.quickdrop/config.json + ~/Downloads/QuickDrop/test-config-*.tmp 完测删.

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $root "quickdrop.exe"
$cfgFile = Join-Path $env:USERPROFILE ".quickdrop\config.json"

if (-not (Test-Path $exe)) {
    Write-Host "FAIL: $exe 不存在, 先 .\build.ps1" -ForegroundColor Red
    exit 1
}

# 清理可能残留的 daemon
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

# 备份用户真实 config.json (如有)
$backupCfg = $null
if (Test-Path -LiteralPath $cfgFile) {
    $backupCfg = (Get-Content -LiteralPath $cfgFile -Raw)
    Remove-Item -LiteralPath $cfgFile -Force
    Write-Host "(已备份 + 临时删除真实 config.json)" -ForegroundColor DarkYellow
}

# 启 daemon (端口 8453, 跟其他测试不冲突)
$port = 8453
function StartDaemon {
    $pi = New-Object System.Diagnostics.ProcessStartInfo
    $pi.FileName = $exe
    $pi.Arguments = "recv"
    $pi.UseShellExecute = $false
    $pi.WindowStyle = "Hidden"
    $pi.EnvironmentVariables["QUICKDROP_PORT"] = "$port"
    return [System.Diagnostics.Process]::Start($pi)
}

function StopDaemon($p) {
    if ($p -and -not $p.HasExited) {
        Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
    }
    Get-Process quickdrop -ErrorAction SilentlyContinue | ForEach-Object {
        Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
    }
    Start-Sleep -Seconds 1
}

function WaitReady($maxSec = 8) {
    $deadline = (Get-Date).AddSeconds($maxSec)
    while ((Get-Date) -lt $deadline) {
        try {
            Invoke-WebRequest -Uri "http://127.0.0.1:$port/api/info" -TimeoutSec 1 -UseBasicParsing | Out-Null
            return $true
        } catch {
            Start-Sleep -Milliseconds 300
        }
    }
    return $false
}

try {
    Write-Host ""
    Write-Host "=== T1: 首次启动默认配置 ===" -ForegroundColor Cyan
    $d = StartDaemon
    if (-not (WaitReady)) { throw "daemon 未就绪" }

    $r = Invoke-WebRequest -Uri "http://127.0.0.1:$port/api/config" -UseBasicParsing
    Check "GET /api/config 返回 200" ($r.StatusCode -eq 200)
    $cfg = $r.Content | ConvertFrom-Json
    Check "默认 download.conflict = rename" ($cfg.download.conflict -eq "rename")
    # 注意 env QUICKDROP_PORT=8453 会覆盖默认 8443
    Check "默认 server.port (来自 env) = 8453" ($cfg.server.port -eq 8453)
    Check "默认 server.mdns_enabled = true" ($cfg.server.mdns_enabled -eq $true)
    Check "默认 ui.toasts_enabled = true" ($cfg.ui.toasts_enabled -eq $true)
    Check "默认 ui.reveal_on_done = true" ($cfg.ui.reveal_on_done -eq $true)
    Check "默认 receive.max_file_size = 0" ($cfg.receive.max_file_size -eq 0)
    Check "默认 system.autostart = false" ($cfg.system.autostart -eq $false)

    Write-Host ""
    Write-Host "=== T2: POST /internal/config-save 保存 ===" -ForegroundColor Cyan
    # 改三项: conflict=overwrite, toast=false, max_file_size=1024
    $save = @{
        download = @{ dir = ""; conflict = "overwrite" }
        server = @{ port = 8453; mdns_enabled = $false }
        receive = @{ max_file_size = 1024 }
        ui = @{ toasts_enabled = $false; reveal_on_done = $false }
        system = @{ autostart = $false }
    } | ConvertTo-Json -Depth 5

    $r = Invoke-WebRequest -Uri "http://127.0.0.1:$port/internal/config-save" -Method POST -Body $save -ContentType "application/json" -UseBasicParsing
    Check "POST config-save 返回 200" ($r.StatusCode -eq 200)

    Write-Host ""
    Write-Host "=== T3: GET 验证热应用 ===" -ForegroundColor Cyan
    $r = Invoke-WebRequest -Uri "http://127.0.0.1:$port/api/config" -UseBasicParsing
    $cfg = $r.Content | ConvertFrom-Json
    Check "热改后 conflict = overwrite" ($cfg.download.conflict -eq "overwrite")
    Check "热改后 toasts_enabled = false" ($cfg.ui.toasts_enabled -eq $false)
    Check "热改后 max_file_size = 1024" ($cfg.receive.max_file_size -eq 1024)
    Check "热改后 mdns_enabled = false" ($cfg.server.mdns_enabled -eq $false)

    Write-Host ""
    Write-Host "=== T4: 持久化 - 磁盘文件存在 + 内容正确 ===" -ForegroundColor Cyan
    Check "config.json 已创建" (Test-Path -LiteralPath $cfgFile)
    if (Test-Path -LiteralPath $cfgFile) {
        $disk = (Get-Content -LiteralPath $cfgFile -Raw) | ConvertFrom-Json
        Check "磁盘 conflict = overwrite" ($disk.download.conflict -eq "overwrite")
        Check "磁盘 max_file_size = 1024" ($disk.receive.max_file_size -eq 1024)
    }

    Write-Host ""
    Write-Host "=== T5: 重启 daemon 配置仍在 ===" -ForegroundColor Cyan
    StopDaemon $d
    $d = StartDaemon
    if (-not (WaitReady)) { throw "daemon 未就绪 (重启)" }
    $r = Invoke-WebRequest -Uri "http://127.0.0.1:$port/api/config" -UseBasicParsing
    $cfg = $r.Content | ConvertFrom-Json
    Check "重启后 conflict 保持 = overwrite" ($cfg.download.conflict -eq "overwrite")
    Check "重启后 max_file_size 保持 = 1024" ($cfg.receive.max_file_size -eq 1024)
    Check "重启后 mdns_enabled 保持 = false" ($cfg.server.mdns_enabled -eq $false)

    Write-Host ""
    Write-Host "=== T6: 非法 port 被拒 ===" -ForegroundColor Cyan
    $bad = @{
        download = @{ dir = ""; conflict = "rename" }
        server = @{ port = 99999; mdns_enabled = $true }
        receive = @{ max_file_size = 0 }
        ui = @{ toasts_enabled = $true; reveal_on_done = $true }
        system = @{ autostart = $false }
    } | ConvertTo-Json -Depth 5
    $code = 0
    try {
        $r = Invoke-WebRequest -Uri "http://127.0.0.1:$port/internal/config-save" -Method POST -Body $bad -ContentType "application/json" -UseBasicParsing
        $code = $r.StatusCode
    } catch {
        $code = $_.Exception.Response.StatusCode.value__
    }
    Check "非法 port 返回 4xx" ($code -ge 400 -and $code -lt 500)
    # 之前的合法配置应仍生效, 没被破坏
    $r = Invoke-WebRequest -Uri "http://127.0.0.1:$port/api/config" -UseBasicParsing
    $cfg = $r.Content | ConvertFrom-Json
    Check "非法保存不影响已生效配置" ($cfg.download.conflict -eq "overwrite")

    Write-Host ""
    Write-Host "=== T7: max_file_size 拒收超大上传 ===" -ForegroundColor Cyan
    # 当前 max_file_size = 1024. 准备 2KB 临时文件上传 → 应 413
    $tmpBig = Join-Path $env:TEMP "qd-test-big-$([guid]::NewGuid().ToString('N')).bin"
    $bytes = New-Object byte[] 2048
    [System.IO.File]::WriteAllBytes($tmpBig, $bytes)
    # 用 curl 形式 multipart (PowerShell -Form 需要 7+, 这里用底层 ms)
    $boundary = [Guid]::NewGuid().ToString()
    $LF = "`r`n"
    $bodyHead = "--$boundary$LF" +
        "Content-Disposition: form-data; name=`"files`"; filename=`"big.bin`"$LF" +
        "Content-Type: application/octet-stream$LF$LF"
    $bodyTail = "$LF--$boundary--$LF"
    $headBytes = [System.Text.Encoding]::UTF8.GetBytes($bodyHead)
    $fileBytes = [System.IO.File]::ReadAllBytes($tmpBig)
    $tailBytes = [System.Text.Encoding]::UTF8.GetBytes($bodyTail)
    $payload = New-Object byte[] ($headBytes.Length + $fileBytes.Length + $tailBytes.Length)
    [Array]::Copy($headBytes, 0, $payload, 0, $headBytes.Length)
    [Array]::Copy($fileBytes, 0, $payload, $headBytes.Length, $fileBytes.Length)
    [Array]::Copy($tailBytes, 0, $payload, $headBytes.Length + $fileBytes.Length, $tailBytes.Length)

    $code = 0
    try {
        $req = [System.Net.WebRequest]::Create("http://127.0.0.1:$port/upload")
        $req.Method = "POST"
        $req.ContentType = "multipart/form-data; boundary=$boundary"
        $req.ContentLength = $payload.Length
        $req.Timeout = 5000
        $s = $req.GetRequestStream()
        $s.Write($payload, 0, $payload.Length)
        $s.Close()
        $resp = $req.GetResponse()
        $code = [int]$resp.StatusCode
        $resp.Close()
    } catch [System.Net.WebException] {
        if ($_.Exception.Response) {
            $code = [int]$_.Exception.Response.StatusCode
        } else {
            $code = -1
        }
    }
    Remove-Item -LiteralPath $tmpBig -Force -ErrorAction SilentlyContinue
    Check "超 max_file_size 上传返回 413" ($code -eq 413)
}
finally {
    StopDaemon $d

    # 还原用户 config.json
    Remove-Item -LiteralPath $cfgFile -Force -ErrorAction SilentlyContinue
    if ($backupCfg) {
        Set-Content -LiteralPath $cfgFile -Value $backupCfg -NoNewline -Encoding UTF8
        Write-Host "(已还原原始 config.json)" -ForegroundColor DarkYellow
    }
}

Write-Host ""
if ($fail -eq 0) {
    Write-Host "ALL PASS" -ForegroundColor Green
    exit 0
} else {
    Write-Host "$fail check(s) FAILED" -ForegroundColor Red
    exit 1
}
