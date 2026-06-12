# 准备验收所需测试文件.
# 跑一次就行, 已存在的不重复生成.

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$repoTest = Join-Path $root "test"

# 1. 中文文件名: 用根目录 test.png 复制成"你好世界.png"
$src = Join-Path $root "test.png"
$dst = Join-Path $repoTest "你好世界.png"
if (-not (Test-Path $src)) {
    Write-Host "ERROR: $src 不存在" -ForegroundColor Red
    exit 1
}
if (Test-Path $dst) {
    Write-Host "你好世界.png 已存在, 跳过"
} else {
    Copy-Item -LiteralPath $src -Destination $dst
    Write-Host "生成: $dst"
}

# 2. 500 MB 随机数据
$big = Join-Path $repoTest "big.bin"
if (Test-Path $big) {
    $size = (Get-Item $big).Length
    if ($size -ge 500MB) {
        Write-Host "big.bin 已存在 ($([Math]::Round($size / 1MB)) MB), 跳过"
    } else {
        Write-Host "big.bin 太小, 重新生成..."
        Remove-Item $big
    }
}
if (-not (Test-Path $big)) {
    Write-Host "生成 500 MB big.bin (这会花几秒)..."
    $fs = [System.IO.File]::Create($big)
    $chunk = New-Object byte[] (1MB)
    $rng = New-Object Random
    $rng.NextBytes($chunk)
    # 同一个 chunk 重复写 500 次, 比每次新随机快很多, 对压缩/MD5 测试也够用
    for ($i = 0; $i -lt 500; $i++) {
        $fs.Write($chunk, 0, $chunk.Length)
    }
    $fs.Close()
    Write-Host "生成: $big ($([Math]::Round((Get-Item $big).Length / 1MB)) MB)"
    $hash = (Get-FileHash $big -Algorithm MD5).Hash
    Write-Host "MD5: $hash (拿来比对手机端下载)"
}

Write-Host ""
Write-Host "fixtures ready:" -ForegroundColor Green
Get-ChildItem -LiteralPath $repoTest -File | Select-Object Name, @{N="Size"; E={"$([Math]::Round($_.Length / 1MB, 2)) MB"}}
