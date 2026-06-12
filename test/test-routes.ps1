# Phase 2.11 + 2.10 UI 收尾验收: ADR-17 路由语义.
#
# 跑法: .\test\test-routes.ps1
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

Write-Host ""
Write-Host "=== Start daemon ===" -ForegroundColor Cyan
$daemon = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 2
if ($daemon.HasExited) {
    Write-Host "FAIL: daemon died" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "=== / dashboard: Vue 骨架 + 静态资源引用, 无下载/上传内联 ===" -ForegroundColor Cyan
$html = (Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/).Content
Check "/ 含 Vue 挂载点 #app" ($html -match 'id="app"')
Check "/ 引用 /assets/index-*.js (Vue chunk)" ($html -match '/assets/index-')
Check "/ 不含 href=`"/file`" 内联 (Vue 端点动态绑定)" (-not ($html -match 'href="/file"'))
Check "/ 不含 <form action=`"/upload`">" (-not ($html -match '<form action="/upload"'))

Write-Host ""
Write-Host "=== /api/info: JSON 当前状态 ===" -ForegroundColor Cyan
$info = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/api/info | Select-Object -ExpandProperty Content | ConvertFrom-Json
Check "/api/info 含 fileName=test.png" ($info.fileName -eq "test.png")
Check "/api/info hasFile=true" ($info.hasFile -eq $true)
Check "/api/info receiving=false (默认)" ($info.receiving -eq $false)
Check "/api/info fileSize 非空" (-not [string]::IsNullOrEmpty($info.fileSize))

Write-Host ""
Write-Host "=== /d 手机端发送页: Vue 骨架, 无内联 QR ===" -ForegroundColor Cyan
$html = (Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/d).Content
Check "/d 含 #app" ($html -match 'id="app"')
Check "/d 引用 /assets/d-*.js" ($html -match '/assets/d-')
Check "/d 不含 <img src=`"/qr`"> (Vue 端点不渲 QR)" (-not ($html -match '<img src="/qr"'))

Write-Host ""
Write-Host "=== /upload 默认 404 (ADR-17 安全约束) ===" -ForegroundColor Cyan
try {
    $r = Invoke-WebRequest -UseBasicParsing -Method Post http://127.0.0.1:8443/upload -Body "x"
    Check "/upload POST 应 404 (实际 $($r.StatusCode))" $false
} catch {
    $code = $_.Exception.Response.StatusCode.value__
    Check "/upload POST 返回 404 (receiveMode 默认 off)" ($code -eq 404)
}
try {
    $r = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/upload
    Check "/upload GET 应 404 (实际 $($r.StatusCode))" $false
} catch {
    $code = $_.Exception.Response.StatusCode.value__
    Check "/upload GET 返回 404" ($code -eq 404)
}

Write-Host ""
Write-Host "=== /qr 仍 200 image/png ===" -ForegroundColor Cyan
$r = Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8443/qr
Check "/qr 状态 200" ($r.StatusCode -eq 200)
Check "/qr Content-Type image/png" ($r.Headers."Content-Type" -eq "image/png")
Check "/qr Body > 100 bytes" ($r.RawContentLength -gt 100)

Write-Host ""
Write-Host "=== /file 仍正常 (HEAD) ===" -ForegroundColor Cyan
$r = Invoke-WebRequest -UseBasicParsing -Method Head http://127.0.0.1:8443/file
Check "/file 状态 200" ($r.StatusCode -eq 200)
Check "/file Content-Disposition 含 test.png" ($r.Headers."Content-Disposition" -match 'test\.png')

Write-Host ""
Write-Host "=== Cleanup ===" -ForegroundColor Cyan
Get-Process quickdrop -ErrorAction SilentlyContinue | ForEach-Object {
    Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
}

Write-Host ""
if ($fail -eq 0) {
    Write-Host "ALL PASS" -ForegroundColor Green
    exit 0
} else {
    Write-Host "FAILED: $fail check(s)" -ForegroundColor Red
    exit 1
}
