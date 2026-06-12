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

**编译**:`go build -o quickdrop.exe`
**运行**:`.\quickdrop.exe send <文件路径>`
**测试文件**:[test.png](./test.png)(1.1 MB)

## 2. 已完成

- [x] 产品定位、技术栈、Phase 1-4 路线、ADR(详见 QuickDrop.md)
- [x] **开发环境迁移**:WSL 交叉编译 → Windows 原生
- [x] **任务 1.1** HTTP 骨架:`/` 路由返回 HTML,监听 `0.0.0.0:8443`
- [x] **任务 1.2** `/file` 下载:`http.ServeFile` + `Content-Disposition: attachment` 强制下载
- [x] **任务 1.3** 网页 QR(已修订,见 QuickDrop.md ADR-13):`/qr` 返回 PNG,主页内嵌 `<img>`,启动自动开浏览器
- [x] **任务 1.5** 命令行:`flag` 包,支持 `send <path>` 显式语法 + 拖拽单参数
- [x] **任务 1.6** `/upload`:`MultipartReader` 流式 + 临时文件 + rename,中文文件名 RFC 5987 编码

## 3. 遇到的问题

- **Windows 防火墙**:第一次跑会弹窗,必须勾"专用网络"和"公用网络"全允许,否则手机连不上
- **控制台中文乱码**:PowerShell 默认 GBK,Go 输出 UTF-8 时日志显示乱码。HTTP 响应里的中文是对的。临时方案 `chcp 65001`
- **`proxy.golang.org` 在国内不通**:已固化 `goproxy.cn`(见 §1)
- **CGO 工具链**:`getlantern/systray` 与 `webview/webview_go` 都依赖 CGO。已装 mingw-w64,`CGO_ENABLED=1` 默认开启

## 4. 待办

### 任务 1.4 — 系统托盘

- 引入 `github.com/getlantern/systray`,启动调 `systray.Run(onReady, onExit)`
- `onReady`:`embed.FS` 嵌 16x16 PNG 图标 + 菜单项"复制扫码链接"、"退出"
- 点"退出"先 HTTP server `Shutdown` 再 `systray.Quit()`

**关键陷阱**:
- `systray.Run` 阻塞调用线程且必须在 main goroutine,HTTP server 要放 `go func() {...}()` 里
- 退出时 HTTP server 必须先 `Shutdown`,不然进程不会退干净

**验收**:右下角任务栏有图标 → 点退出 → 任务管理器看不到 `quickdrop.exe`

### 任务 1.7 — Phase 1 验收

跨平台编译命令写进 `Makefile` 或 `build.ps1`,在 Windows 实地走完整流程 5 次,写一份 `TEST.md`。

验收用例:
1. 小图(<1MB)发送 — 用 [test.png](./test.png)
2. 大文件(>500MB)发送
3. 中文文件名发送
4. 中文文件名上传
5. 传输中点托盘退出 → 程序应干净退出且不留临时文件
6. 同时多个手机访问(行为 OK 即可,并发问题留 Phase 2)

**Phase 1 完成标志**:上面 6 条都过了,能放心把 .exe 给一个朋友说"扫码就能给我发文件"。
