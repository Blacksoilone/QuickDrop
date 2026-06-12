# QuickDrop 执行进度

> 产品蓝图、ADR、Phase 2-4 规划见 [QuickDrop.md](./QuickDrop.md)。
> 本文档只写**进度、环境、遇到的问题、待办**。

## 1. 开发环境

| 项 | 值 |
|---|---|
| 工作区 | `D:\go_workspace\QuickDrop` |
| Go | 1.26 windows/amd64 (`C:\Program Files\Go\bin`) |
| GOPROXY | `https://goproxy.cn,direct` |
| gcc (CGO) | mingw-w64 16.1.0 ucrt (`scoop install mingw`) |
| Node / npm | 24.16 / 11.13 (Phase 2.7 才用) |
| WebView2 Runtime | 149.0(Win11 自带,Phase 2.10 才用) |

**编译**:`.\build.ps1`(release,隐藏黑窗) 或 `.\build.ps1 -Debug`(控制台可见)
**运行**:`.\quickdrop.exe send <文件路径>`
**测试文件**:[test.png](./test.png)(1.1 MB,仓库自带);其余固件用 `.\test\prepare-fixtures.ps1` 生成
**日志**:`%TEMP%\quickdrop.log`(`-H=windowsgui` 下 stderr 无效,统一写文件)

## 2. 项目结构

```
cmd/quickdrop/main.go         入口,probeDaemon → 客户端模式 IPC / daemon 模式起服务
internal/server/server.go     HTTP server (Start/Shutdown/SwapFile), 路由含 /internal/{health,send}
internal/server/templates.go  HTML 模板
internal/qr/qr.go             QR PNG 渲染
internal/tray/tray.go         systray, 菜单(复制链接/退出), UpdateTooltip
internal/tray/icon.ico        托盘图标 16x16 (Windows systray 必须 ICO,不能 PNG)
build.ps1                     一键构建脚本
test/prepare-fixtures.ps1     生成 500MB big.bin + 你好世界.png 验收固件
test/test-crash-cleanup.ps1   验收用例 5 自动化 (中途崩溃 → 重启清理)
test/test-daemon-switch.ps1   Phase 2.2+2.3 验收: daemon 健康检查 / 客户端 IPC 切换 / requireLocal
TEST.md                       Phase 1 验收清单 (手动 + 自动)
```

## 3. 已完成

- [x] 产品定位、技术栈、Phase 1-4 路线、ADR(详见 QuickDrop.md)
- [x] **开发环境迁移**:WSL 交叉编译 → Windows 原生
- [x] **任务 1.1** HTTP 骨架:`/` 路由返回 HTML,监听 `0.0.0.0:8443`
- [x] **任务 1.2** `/file` 下载:`http.ServeFile` + `Content-Disposition: attachment` 强制下载
- [x] **任务 1.3** 网页 QR(已修订,见 QuickDrop.md ADR-13):`/qr` 返回 PNG,主页内嵌 `<img>`
- [x] **任务 1.4** 系统托盘:`getlantern/systray` + `atotto/clipboard`,菜单"复制扫码链接"/"退出",删掉 Phase 1 自动开浏览器代码(ADR-11)
- [x] **任务 1.5** 命令行:`flag` 包,支持 `send <path>` 显式语法 + 拖拽单参数
- [x] **任务 1.6** `/upload`:`MultipartReader` 流式 + 临时文件 + rename,中文文件名 RFC 5987 编码
- [x] **任务 1.7 验收**:用例 1-5 全部 ✅,用例 6(多手机并发)缺设备暂缓。Phase 1 实质完成
- [x] **任务 2.1** 拆包:`cmd/quickdrop` + `internal/{server,qr,tray}`(见 §2)
- [x] **任务 2.2 + 2.3** Daemon + IPC:第二次 `quickdrop send Y` 不重启进程,POST `/internal/send` 让运行中的 daemon 切换文件。`/internal/*` 由 `requireLocal` 限制只 127.0.0.1 可访问,LAN 一律 404。tooltip 自动跟随当前文件。`test-daemon-switch.ps1` 自动 PASS

## 4. 遇到的问题

- **Windows 防火墙**:第一次跑会弹窗,必须勾"专用网络"和"公用网络"全允许,否则手机连不上
- **控制台中文乱码**:PowerShell 默认 GBK,Go 输出 UTF-8 时日志显示乱码。HTTP 响应里的中文是对的。临时方案 `chcp 65001`
- **`proxy.golang.org` 在国内不通**:已固化 `goproxy.cn`(见 §1)
- **CGO 工具链**:`getlantern/systray` 与 `webview/webview_go` 都依赖 CGO。已装 mingw-w64,`CGO_ENABLED=1` 默认开启
- **systray.SetIcon 在 Windows 必须 ICO**:传 PNG 报 `Unable to set icon: The operation completed successfully.`,SetTitle 兜底文字仍可见但无图标。解决:用 PNG-in-ICO 容器(Vista+ 支持)。`internal/tray/icon.ico` 是临时占位
- **`-H=windowsgui` 下 stderr 失效**:`io.MultiWriter(os.Stderr, f)` 在 stderr 无效时 short-circuit,文件那段永远写不到。解决:`log.SetOutput(f)` 只写文件,调试时 tail `%TEMP%\quickdrop.log`
- **崩溃留 `.tmp` 残留**:`taskkill /F` 跳过 saveStream 的 defer 清理 → 半截 tmp 留在 Downloads/QuickDrop。解决:server.Start 启动时 `cleanupStaleTmp()` 清扫上次残留
- **PowerShell 5.1 读 UTF-8 ps1 报中文乱码**:必须存为 UTF-8 **with BOM**,无 BOM 会被当 GBK 解析炸 parser。已固化
- **旧版 daemon 占着 8443 时新版起不来**:probeDaemon 只认 `X-QuickDrop:1` header,旧版没此 header → 判定"不是 daemon"→ 走 daemon 模式 → bind fail。**升级场景需要先 kill 旧版**。后续可考虑 daemon 模式 bind fail 时给用户友好提示

## 5. 待办

### 下一步 — 2.10 WebView2 小窗 (推荐)

发送方电脑端目前**没有 UI** (删了自动开浏览器后)。用户得自己记 URL 或扫自己屏幕背面的 QR。
做法见 [QuickDrop.md §6 Phase 2 2.10](./QuickDrop.md):
- `github.com/webview/webview_go` 起 280×320 无边框小窗加载 `http://localhost:8443/qr`
- 窗口顶部小字 "扫码或选择设备" + QR
- 点 QR 之外区域 → 关窗 (但 daemon 仍在跑)

### 待补验收

- ⏸ TEST.md 用例 6 多手机并发(等借到第二台手机)

### 后续 Phase 2 顺位

- 2.11 UI 分化(`/d` `/u` 路由拆分,见 QuickDrop.md ADR-14)
- 2.7 Vue 工程化
- 2.4 Windows 右键菜单(注册表写入,有了之后才真正"无感")
- 2.5 mDNS 发现 PC / 2.6 设备记忆
- 2.8 WebSocket 进度 / 2.9 Windows toast

详情见 [QuickDrop.md §6](./QuickDrop.md)。

### 后续 Phase 2 才做的小项

- 托盘图标用绘图软件画一个像样的(目前是纯蓝 16x16 占位)
- daemon 模式发现端口被占但响应不是 QuickDrop 时,给用户更友好的报错 (现状是 ListenAndServe `log.Fatalf`,见旧版残留 daemon 那次踩坑)
