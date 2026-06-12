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

**编译**:`go build -ldflags="-H=windowsgui" -o quickdrop.exe ./cmd/quickdrop`
**运行**:`.\quickdrop.exe send <文件路径>`
**测试文件**:[test.png](./test.png)(1.1 MB)
**日志**:`%TEMP%\quickdrop.log`(`-H=windowsgui` 下 stderr 无效,统一写文件)

## 2. 项目结构

```
cmd/quickdrop/main.go         入口,参数解析,日志,组装 server+tray
internal/server/server.go     HTTP server (Start/Shutdown), 路由, LAN IP, 文件保存
internal/server/templates.go  HTML 模板
internal/qr/qr.go             QR PNG 渲染
internal/tray/tray.go         systray, 菜单(复制链接/退出)
internal/tray/icon.ico        托盘图标 16x16 (Windows systray 必须 ICO,不能 PNG)
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
- [x] **任务 2.1** 拆包:`cmd/quickdrop` + `internal/{server,qr,tray}`(见 §2)

## 4. 遇到的问题

- **Windows 防火墙**:第一次跑会弹窗,必须勾"专用网络"和"公用网络"全允许,否则手机连不上
- **控制台中文乱码**:PowerShell 默认 GBK,Go 输出 UTF-8 时日志显示乱码。HTTP 响应里的中文是对的。临时方案 `chcp 65001`
- **`proxy.golang.org` 在国内不通**:已固化 `goproxy.cn`(见 §1)
- **CGO 工具链**:`getlantern/systray` 与 `webview/webview_go` 都依赖 CGO。已装 mingw-w64,`CGO_ENABLED=1` 默认开启
- **systray.SetIcon 在 Windows 必须 ICO**:传 PNG 报 `Unable to set icon: The operation completed successfully.`,SetTitle 兜底文字仍可见但无图标。解决:用 PNG-in-ICO 容器(Vista+ 支持)。`internal/tray/icon.ico` 是临时占位
- **`-H=windowsgui` 下 stderr 失效**:`io.MultiWriter(os.Stderr, f)` 在 stderr 无效时 short-circuit,文件那段永远写不到。解决:`log.SetOutput(f)` 只写文件,调试时 tail `%TEMP%\quickdrop.log`
- **`Stop-Process -Force` 不会触发 Shutdown**:测试时强杀进程跳过 systray onExit → server.Shutdown 也跳过。真实退出路径是用户点托盘"退出"菜单,需要 GUI session 才能验证

## 5. 待办

### 任务 1.7 — Phase 1 验收

跨平台编译命令写进 `Makefile` 或 `build.ps1`,在 Windows 实地走完整流程 5 次,写一份 `TEST.md`。

验收用例:
1. 小图(<1MB)发送 — 用 [test.png](./test.png)
2. 大文件(>500MB)发送
3. 中文文件名发送
4. 中文文件名上传
5. 传输中点托盘"退出" → 程序应干净退出且不留临时文件(`%TEMP%` 里检查 `*.tmp`)
6. 同时多个手机访问(行为 OK 即可,并发问题留 Phase 2)

**Phase 1 完成标志**:上面 6 条都过了,能放心把 .exe 给一个朋友说"扫码就能给我发文件"。

### 后续(Phase 2 才做)

托盘图标用绘图软件画一个像样的(目前是纯蓝 16x16 占位),Phase 2.10 WebView2 小窗实装时一并替换。
