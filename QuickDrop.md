# QuickDrop

> 本文档是产品蓝图。**写"要做成什么样"**,不写"进度到哪了"。
> 进度看 [PLAN.md](./PLAN.md)。

## 0. 一句话

Windows 上没有的 AirDrop —— 右键文件即发送,扫码即接收,局域网内 PC 互相自动发现。

## 1. 核心体验目标(量化"无感")

- 右键文件 → 菜单"通过 QuickDrop 发送" → **1 秒内**弹窗
- 弹窗同时显示:① 二维码(手机扫) ② 局域网内已发现的 PC 列表(点谁发谁)
- 点目标后立即开始传输,传完自动关弹窗
- 接收方桌面右下角弹通知"小明的电脑想发 photo.jpg (2.3MB) [接收] [拒绝]"
- 手机端零安装,**系统相机扫码** → 浏览器即用

## 2. 明确不做的事

- ❌ 手机 ↔ 手机
- ❌ 公网穿透 / NAT 打洞 / 云中转
- ❌ 帐号系统 / 登录
- ❌ macOS、Linux 桌面集成(MVP 阶段)
- ❌ 文件夹镜像同步(只做"发送/接收"动作)

## 3. 使用模式(双端角色不对等)

电脑是"决定者",手机是"被动客户端"。一个 QR 对应一个动作,**手机端不存在同时能下载又上传的页面**。

| 模式 | 主入口 | 电脑端展示 | 手机端(扫 QR 后) |
|---|---|---|---|
| 发送 (PC → 手机) | **右键文件 → "通过 QuickDrop 发送"** | 无边框小窗显当前文件 + QR | 仅**下载页**(文件名 + 下载按钮,无上传入口) |
| 接收 (手机 → PC) | **点托盘图标 → 配置页"接收文件"** | 小窗 / 配置页内显 QR | 仅**上传页**(上传表单,无下载链接) |

## 4. 运行形态(理想态)

- 程序常驻系统托盘,右下角一个小图标 — **平时不弹任何主窗口**
- 右键发送是主交互,等同 Windows 用户分享文件的肌肉记忆;传完小窗自动消失,程序仍常驻等下次
- 点托盘图标 → 一个很简单的**配置页**:
  - 参数:端口、下载根目录、设备名、开机自启、接收前确认
  - 按钮:"发送文件" / "接收文件"

## 5. 技术栈(已定档,不再争论)

| 用途 | 选型 | 备注 |
|---|---|---|
| HTTP 服务器 | `net/http` 标准库 | 单聚焦工具,不上 Gin/Echo |
| WebSocket | `github.com/coder/websocket` | gorilla 已 archive,coder 是继任 |
| 二维码 | `github.com/skip2/go-qrcode` | PNG/SVG 输出 |
| 系统托盘 | `github.com/getlantern/systray` | Wails v2 没原生托盘,v3 alpha 风险高 |
| 自有窗口 | `github.com/webview/webview_go` + Windows WebView2 | 复用 `/qr` 路由,不打包浏览器 |
| mDNS 服务发现 | `github.com/betamos/zeroconf` | Phase 2 才用 |
| 前端框架 | Vue 3 + Vite | Phase 2 才用,Phase 1 写死 HTML |
| 前端打包 | Go 1.16+ `embed.FS` | 单二进制分发关键 |

**禁用清单**:
- ❌ Wails(托盘问题)
- ❌ WebRTC(iOS Safari 在 LAN IP 上禁用安全上下文)
- ❌ Gin / Echo / Fiber(没必要)
- ❌ gorilla/websocket(已 archived)
- ❌ Cobra(MVP `flag` 包够了)

## 6. Phase 路线

### Phase 1 — 最小可跑通回路

单文件 `main.go`,Windows 双击就能跑。**不分包**。预期 150-250 行 Go。

**验收**:
- `.\quickdrop.exe send C:\path\test.jpg`
- 手机扫 → 浏览器弹下载 → 文件到手
- 反向:手机扫 → 选文件上传 → 文件出现在 Windows `Downloads/QuickDrop/`

子任务:1.1 HTTP 骨架 / 1.2 `/file` 下载 / 1.3 网页 QR / 1.4 系统托盘 / 1.5 命令行 / 1.6 `/upload` / 1.7 验收。

### Phase 2 — 系统集成 + Vue 前端

- **2.1** 拆包:`cmd/quickdrop/main.go` + `internal/server` + `internal/qr` + `internal/tray`
- **2.2** Daemon 模式:第一次启动常驻;后续 `quickdrop send X` 检测 daemon 已跑就走 IPC,不重启进程
- **2.3** IPC:HTTP 到 `127.0.0.1:8443/internal/send`,先不上 Named Pipe
- **2.4** Windows 右键菜单:写注册表 `HKCU\Software\Classes\*\shell\QuickDrop\command`,值 `"C:\path\quickdrop.exe" send "%1"`
- **2.5** mDNS 广播 + 发现 PC 列表
- **2.6** 设备记忆:JSON `~/.quickdrop/devices.json`
- **2.7** Vue 3 工程化:`web/` 目录,vite 构建,产物用 `embed.FS` 打进 .exe
- **2.8** WebSocket 实时进度
- **2.9** Windows toast 通知接收方
- **2.10** 自有窗口替代浏览器:WebView2 起 280×320 无边框小窗加载 `http://localhost:8443/qr`,删 Phase 1 自动开浏览器代码
- **2.11** 电脑端 / 手机端 UI 分化 + 模式选择(见 ADR-14):
  - **路由设计**:
    - `/`  电脑端 dashboard,无参时显示"发送/接收"选择,模式定后显当前 QR + 状态
    - `/d` 手机端下载页,发送模式 QR 指向这里,仅文件信息 + 下载按钮
    - `/u` 手机端上传页,接收模式 QR 指向这里,仅上传表单
    - `/file` `/upload` `/qr` 保留为内部资源路由
  - **模式参数**:
    - `quickdrop send <path>` 或裸路径 → 发送模式,QR 编码 `http://<lan>:8443/d`
    - `quickdrop recv` 或双击选"接收" → 接收模式,QR 编码 `http://<lan>:8443/u`
  - **无参 + 选"发送"时的文件对话框**:
    - 短期:HTML `<input type="file">` → 暂存 `os.TempDir()` → 由 `/d` 暴露(代价:文件先经一次"上传到自己")
    - 长期(配合 2.10):窗口内 JS 调 Win32 `GetOpenFileName` 或 File System Access API 拿磁盘原始路径

### Phase 3 — 适配朋友机型

- HTTPS via [lancert.dev](https://github.com/lucor/lancert) 通配证书,绑域名 `192-168-x-x.lancert.dev`
- PWA manifest + Service Worker(只缓存壳)
- iOS HEIC 浏览器端 `heic2any` 转 JPEG 再上传
- **微信内置浏览器适配**(中国市场最大坑,需要单独投入)
- NoSleep.js 防熄屏
- 多机型 QA 矩阵

### Phase 4 — 体验打磨(开放)

- Win11 顶级右键菜单(MSIX sparse package + IExplorerCommand,工程量较大)
- macOS Automator Service / Quick Action
- Linux Nautilus / Dolphin 集成
- 自动更新
- 剪贴板共享
- 多语言

## 7. 决策记录(ADR)

为避免反复讨论已决事项,记录在此。要推翻请明说。

| ID | 决定 | 理由 |
|---|---|---|
| 1 | 不用 WebRTC | iOS Safari 在 LAN IP 上禁用安全上下文,LAN HTTP 反而吞吐更高 |
| 2 | 不用 Wails | v2 没托盘 API,v3 alpha 三年了 |
| 3 | HTTP 直连而非中继 | 服务器本身就是其中一端,中继毫无意义 |
| 4 | Phase 1 单文件 main.go | 不期望一开始就有合理结构,先跑通再分层 |
| 5 | Phase 1 不上 Vue | HTML 字符串够了,先验证后端骨架 |
| 6 | Windows 平台优先 | 目标用户也是 Windows 居多 |
| 7 | 仅 net/http 标准库 | 单聚焦工具不需要框架 |
| 8 | 二进制名 `quickdrop.exe` | 短、好记、符合命令行习惯 |
| 9 | 端口 8443 | 不冲突常见服务,数字像 https 端口暗示后期会上 TLS |
| 10 | 接收路径 `~/Downloads/QuickDrop/` | 跨平台都有 Downloads,自带子目录避免污染 |
| 11 | 最终 UI 必须是自有窗口,**不允许打开浏览器** | 浏览器有地址栏、标签页、需要找窗口,破坏"无感"。Phase 1 临时用浏览器,Phase 2 必改 |
| 12 | Phase 2 自有窗口方案: **WebView2 (Windows 自带)** + 复用 `/qr` 路由 | Windows 10+ 系统自带 Edge WebView2,不需打包浏览器二进制。`github.com/webview/webview_go` 几行代码起无地址栏小窗 |
| 13 | **推翻 1.3 "终端打印 QR"**,改为网页渲染 `/qr` PNG + 主页内嵌 `<img>` | 终端 QR 与最终形态(WebView2 显示同一张 PNG)路径相反,是死胡同。网页 QR 直接复用同一资源 |
| 14 | **电脑端 / 手机端 UI 分化**: 一个 QR 对应一个动作 | 电脑是决定者、手机是被动客户端。用 URL 路径区分用途比 UA 嗅探稳,方便 Phase 3 PWA。**部分推翻 ADR-13** 的"一份页面服务两端",ADR-13 在 Phase 1 仍成立,Phase 2 实现 2.11 时按本条拆分 |
