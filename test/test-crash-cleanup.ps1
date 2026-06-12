# 验收用例 5 (自动化): 上传中强杀进程, 验证下次启动清理残留 .tmp.
#
# 用法: .\test\test-crash-cleanup.ps1
# 退出码: 0 PASS, 1 FAIL

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $root "quickdrop.exe"
$testPng = Join-Path $root "test.png"
$downloads = Join-Path $env:USERPROFILE "Downloads\QuickDrop"
$bigFile = Join-Path $env:TEMP "crashtest_big.bin"

if (-not (Test-Path $exe)) {
    Write-Host "FAIL: $exe 不存在, 先跑 .\build.ps1" -ForegroundColor Red
    exit 1
}
if (-not (Test-Path $testPng)) {
    Write-Host "FAIL: $testPng 不存在" -ForegroundColor Red
    exit 1
}

# 清空 downloads dir
if (Test-Path $downloads) {
    Get-ChildItem $downloads -File | Remove-Item -Force
}

Write-Host "=== A: start server, fire throttled upload, kill mid-flight ==="
$proc = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 1
if ($proc.HasExited) {
    Write-Host "FAIL: server 没启动起来" -ForegroundColor Red
    exit 1
}

# 准备 100 MB 文件(配合 --limit-rate 5M ≈ 20 秒, 杀的时机更可靠)
$fs = [System.IO.File]::Create($bigFile)
$chunk = New-Object byte[] (1MB)
for ($i = 0; $i -lt 100; $i++) { $fs.Write($chunk, 0, $chunk.Length) }
$fs.Close()

$curl = Start-Process -FilePath "curl.exe" -ArgumentList "--limit-rate","5M","-X","POST","-F","file=@$bigFile","-s","--max-time","60","http://127.0.0.1:8443/upload" -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 2
Stop-Process -Id $proc.Id -Force
Start-Sleep -Seconds 1
if (-not $curl.HasExited) { Stop-Process -Id $curl.Id -Force -ErrorAction SilentlyContinue }

$residueBefore = @(Get-ChildItem $downloads -Filter "*.tmp" -ErrorAction SilentlyContinue)
Write-Host "After kill, .tmp residue count = $($residueBefore.Count)"
if ($residueBefore.Count -eq 0) {
    Write-Host "WARN: 没造出 .tmp 残留, 测试条件没满足 (可能 limit-rate 没生效或太快)" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "=== B: restart server, expect cleanup ==="
$proc2 = Start-Process -FilePath $exe -ArgumentList "send",$testPng -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 2
$residueAfter = @(Get-ChildItem $downloads -Filter "*.tmp" -ErrorAction SilentlyContinue)
Stop-Process -Id $proc2.Id -Force -ErrorAction SilentlyContinue

# 清掉测试副产物
Remove-Item $bigFile -ErrorAction SilentlyContinue
Get-ChildItem $downloads -File | Remove-Item -Force -ErrorAction SilentlyContinue

if ($residueAfter.Count -eq 0) {
    Write-Host "PASS: directory clean" -ForegroundColor Green
    exit 0
} else {
    Write-Host "FAIL: 重启后仍有 .tmp 残留:" -ForegroundColor Red
    $residueAfter | Select-Object Name, Length
    exit 1
}
