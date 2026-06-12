# QuickDrop Phase 1 验收

> 6 项验收用例。
> 走完且全部 ✅ 即可宣布 Phase 1 完成,把 .exe 给朋友试。

**当前状态(2026-06-12)**: 用例 1-5 已通过,用例 6 因没有第二台手机暂时挂起。
Phase 1 实质完成,等用例 6 设备齐了再补勾。

## 准备

```powershell
# 构建
.\build.ps1

# 准备测试文件 (一次性):
#   test.png          仓库自带, 1.1 MB
#   你好世界.png      复制 test.png → 改中文名
#   big.bin           500 MB 随机数据
.\test\prepare-fixtures.ps1
```

发送验证步骤都长这样:
1. `.\quickdrop.exe send <文件>` 双击或拖拽
2. 托盘出现蓝色图标 → 右键 → "复制扫码链接" → 粘出 `http://192.168.x.x:8443/`
3. 同 WiFi **手机**用相机扫主页 QR(或直接打开复制的 URL)
4. 手机看到主页 → 点"下载"
5. 文件下载到手机相册/下载

上传验证步骤:
1. 仍在主页, 滑到"上传到电脑"
2. 选文件 → "上传"
3. 等"已收到 N 个文件"页面
4. Windows 上看 `~\Downloads\QuickDrop\` 应出现文件

---

## 验收用例

### ☑ 1. 小图(<1MB)发送

```powershell
.\quickdrop.exe send test.png
```

- ☐ 托盘图标出现
- ☐ 主页 QR 渲染
- ☐ 手机扫码进主页, 文件名显示 `test.png (1.1 MB)`
- ☐ 手机点"下载" → 收到完整 PNG, 大小 1,141,522 字节
- ☐ 点托盘"退出" → 任务管理器 5 秒内看不到 quickdrop.exe

### ☑ 2. 大文件(>500MB)发送

```powershell
.\quickdrop.exe send .\test\big.bin
```

- ☐ 主页文件大小显示 `500.0 MB`
- ☐ 手机能完成下载(可能要几分钟视 WiFi)
- ☐ 下载过程中电脑 quickdrop.exe **内存稳定**(任务管理器查, 不应随传输线性增长)
- ☐ 下载完整后, 手机端 MD5 与 `Get-FileHash .\test\big.bin` 一致

### ☑ 3. 中文文件名发送

```powershell
.\quickdrop.exe send .\test\你好世界.png
```

- ☐ 主页"下载到此设备"区域文件名显示 `你好世界.png` (不乱码)
- ☐ 手机端浏览器下载保存的文件名也是 `你好世界.png` (验证 RFC 5987)

### ☑ 4. 中文文件名上传

任意状态 `quickdrop.exe send ...` 启动后:

- ☐ 手机端选一个本地图片改名为含中文(如 `测试照片.jpg`)上传
- ☐ Windows `~\Downloads\QuickDrop\` 出现 `测试照片.jpg` (不是乱码 / 不是问号)

### ☑ 5. 传输中点退出 → 不留临时文件(已自动化)

`build.ps1` 之后跑:

```powershell
.\test\test-crash-cleanup.ps1
```

预期输出 `PASS: directory clean`。
原理: 启动一个 server, 发起大上传, 中途 `Stop-Process -Force` 杀掉, 检查 `Downloads\QuickDrop` 留 `.tmp`. 然后重启 server, 期望启动清理把 `.tmp` 清掉。
注: 残留 `.tmp` 不是 bug, 是 OS 强杀必然结果 — 修复策略是**下次启动时清扫**, 见 server.go `cleanupStaleTmp`。

### ⏸ 6. 多手机并发 (暂缓: 缺第二台手机)

`quickdrop.exe send test.png` 启动后:

- ☐ 两台手机同时打开主页 → 都能渲染
- ☐ 两台手机几乎同时点"下载" → 都能完成
- ☐ server 日志没崩, `quickdrop.log` 没新报错

> 真正的并发问题(连接限制、QR 缓存等)留 Phase 2 评估, 此处只要"不崩"。

---

## 完成

全部勾上 → Phase 1 完成 → 可以把 .exe 发给一个朋友, 说 "扫码就能给我发文件"。
