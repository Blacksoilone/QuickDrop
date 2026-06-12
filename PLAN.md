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
cmd/quickdrop/main.go         入口,window 子命令 + probeDaemon → client/daemon
internal/server/server.go     HTTP server (Start/Shutdown/SwapFile), /internal/{health,send}
internal/server/templates.go  HTML 模板
internal/qr/qr.go             QR PNG 渲染
internal/tray/tray.go         systray, 菜单(复制链接/退出), UpdateTooltip
internal/tray/icon.ico        托盘图标 16x16 (Windows systray 必须 ICO,不能 PNG)
internal/window/window.go     webview 子进程实际渲染函数 (runWebview)
internal/window/manager.go    daemon 持有的子进程管理器 + Mode (replace/keep/first-only)
build.ps1                     一键构建脚本
test/prepare-fixtures.ps1     生成 500MB big.bin + 你好世界.png 验收固件
test/test-crash-cleanup.ps1   验收用例 5 自动化 (中途崩溃 → 重启清理)
test/test-daemon-switch.ps1   Phase 2.2+2.3 验收: daemon 健康检查 / 客户端 IPC 切换
test/test-window.ps1          Phase 2.10 验收: webview 子进程 + 三种 window-mode
TEST.md                       Phase 1 验收清单 (手动 + 自动)
```

**环境变量**:
- `QUICKDROP_WINDOW_MODE=replace|keep|first-only` 控制 daemon 弹窗策略,默认 replace

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
- [x] **任务 2.1** 拆包:`cmd/quickdrop` + `internal/{server,qr,tray,window}`(见 §2)
- [x] **任务 2.2 + 2.3** Daemon + IPC:第二次 `quickdrop send Y` 不重启进程,POST `/internal/send` 让运行中的 daemon 切换文件。`/internal/*` 由 `requireLocal` 限制只 127.0.0.1 可访问,LAN 一律 404。tooltip 自动跟随当前文件。`test-daemon-switch.ps1` 自动 PASS
- [x] **任务 2.10** WebView2 小窗:daemon fork `quickdrop window <url>` 子进程,WebView2 280×320 固定大小加载主页 QR。删除"自动开浏览器"(ADR-11 真正落地)。子进程独立 main goroutine 避开 systray/webview 消息循环冲突。三种 window-mode(replace/keep/first-only)由 `QUICKDROP_WINDOW_MODE` env 控制,`test-window.ps1` 自动 PASS

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
- **systray + webview 不能同进程**:两者都要 main goroutine 跑 Windows 消息循环,放一起会抢消息/调试地狱(见 webview issue #650, systray issue #195)。**方案**:webview 单独 fork 子进程,daemon 进程持 PID 管理生命周期(replace/keep/first-only 三策略)

## 5. 待办

### 下一步 — 2.11 路由分化 + 2.10 UI 收尾 (按 ADR-17)

实际用了一下 v0.3.0 发现 UI 不行: 上下太长, 没固定宽度需要滑动才看到完整 QR, 电脑端弹窗里居然有上传区块。**已写入 ADR-17 修订**:

1. **电脑端弹窗** (2.10 UI 收尾):
   - 无边框、固定 ~280px 宽
   - 只显示: **QR + 文件名 + 大小 + 关闭按钮**
   - 没有"下载到此设备"区块 (电脑端不需要下载给自己)
   - 没有"上传到电脑"区块 (上传留给 2.13 接收模式)
2. **路由分化** (2.11):
   - `/` → 电脑端 dashboard, 极简版 (上一条)
   - `/d` → 手机端发送目标页, **用文件图标/缩略图替代 QR**, 仅显示文件信息 + 下载按钮
   - `/u` → 手机端上传页, **默认 404, 仅接收模式开启时存在** (见 2.13)
3. **2.13 接收模式 + 安全分离** (ADR-17 新增):
   - 默认 `/u` 不存在,任何陌生人扫到发送 QR 都不能向 `~/Downloads/QuickDrop/` 塞文件 (解决当前隐患)
   - 入口: `quickdrop recv` / 托盘"接收文件"菜单 / 配置页按钮
   - 接收模式弹独立窗 → 显示接收 QR → 完成或停止后 `/u` 立即注销
4. **2.12 托盘菜单扩充**: 加 "接收文件" / "打开配置", 复杂功能从弹窗剥离

详见 [QuickDrop.md §6 + ADR-17](./QuickDrop.md)。

### 待补验收

- ⏸ TEST.md 用例 6 多手机并发(等借到第二台手机)

### 后续 Phase 2 顺位

- 2.7 Vue 工程化(2.11 路由设计敲定后再上 Vue)
- 2.4 Windows 右键菜单(注册表写入,有了之后才真正"无感")
- 2.5 mDNS 发现 PC / 2.6 设备记忆
- 2.8 WebSocket 进度 / 2.9 Windows toast

### 后续 Phase 2 才做的小项

- 托盘图标用绘图软件画一个像样的(目前是纯蓝 16x16 占位)
- daemon 模式发现端口被占但响应不是 QuickDrop 时,给用户更友好的报错
- `QUICKDROP_WINDOW_MODE` 配置项最终应进配置页 UI (现在只能 env 设置)
