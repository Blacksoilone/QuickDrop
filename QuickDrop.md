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
| mDNS 服务发现 | `github.com/grandcat/zeroconf` | Phase 2.5a, 比 betamos 维护更活跃 |
| Windows toast 通知 | `github.com/go-toast/toast` | Phase 2.5c, 原生 actions 支持 (接受/拒绝按钮) |
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
- **2.4** Windows 右键菜单(已实现 v0.6.0):`quickdrop install` 写注册表 `HKCU\Software\Classes\*\shell\QuickDrop\command`,值 `"C:\path\quickdrop.exe" send "%1"`。`uninstall` 删,`status` 看。用户级 (HKCU) 无需 UAC。**Win11 限制**:传统注册表方式在 Win11 下显示在 "显示更多选项" 里 (Shift+右键直达),顶级菜单需要 MSIX sparse package + IExplorerCommand (留 Phase 4,见 ADR-18)
- **2.5** mDNS 发现 + PC→PC 互传 (ADR-19, ADR-20): 拆 5 个子步骤
  - **2.5a** mDNS 广播 + 发现: `github.com/grandcat/zeroconf`, 服务名 `_quickdrop._tcp.local`,
    广播主机名 + 用户起的显示名 + LAN IP + 端口。daemon 起 server 时一并起 mDNS,
    托盘菜单 / 配置页 / 弹窗里能看到"局域网内的 QuickDrop 实例" 列表
  - **2.5b** 设备间 IPC 协议: 新路由 `/peer/incoming` POST (来自其他 PC), body =
    `{ from: "老王的电脑", id: "uuid", fileName: "x.png", fileSize: 1234567 }`,
    daemon 暂存到内存 pending queue, 等用户接受/拒绝。发送方对应新路由 `/peer/file?id=xxx`
    暴露文件 (有 token, 不复用 `/file` 防误用)
  - **2.5c** Toast 通知: 引 `github.com/go-toast/toast`, 收到 `/peer/incoming` 后
    弹 toast "老王的电脑 想发 x.png (1.2MB) [接受] [拒绝]", actions 走 `quickdrop://`
    URL scheme; install 时注册 `HKCU\Software\Classes\quickdrop\shell\open\command`
  - **2.5d** quickdrop:// URL handler: `quickdrop accept --id xxx` / `quickdrop reject --id xxx`
    新 CLI 子命令, 通过 IPC POST 给 daemon `/internal/peer-decide`,
    accept 时 daemon 主动 GET `http://发送方:8443/peer/file?id=xxx` Pull 文件到 `~/Downloads/QuickDrop/`,
    reject 时回 `/peer/decision?id=xxx&decision=reject` 让发送方知道
  - **2.5e** 红点 fallback: 托盘 tooltip 改 "QuickDrop - 1 个待处理", 菜单加
    "待处理文件 (1)" 项, 点击打开独立 webview 列表页 (`/pending`), 用户可批量接受/拒绝
- **2.6** 设备记忆 + 信任升级 (配合 ADR-20):
  - JSON 文件 `~/.quickdrop/devices.json` 记录已交互过的 PC 及"是否信任"
  - 配置页 / `/pending` 页加 "信任此设备" 复选框, 勾上后该设备发文件:
    toast 仍弹但 3 秒后自动接受 (中途用户可点"拒绝"撤销)
  - 加 "永不信任此设备" 黑名单, 黑名单内设备发文件: toast 不弹, daemon 直接回 reject
- **2.7** Vue 3 工程化(已实现 v0.7.0):`web/` 目录 Vite + Vue3 + TS MPA (4 个独立入口对应 / /d /r /u 4 路由),Dashboard/Download/Receive/Upload.vue 替代 HTML 模板;构建产物 `web/dist/` 由 build.ps1 复制到 `cmd/quickdrop/web/` 用 `embed.FS` 打进 .exe;新增 `/api/info` JSON API 供 Vue 拉服务状态
- **2.8** WebSocket 实时进度
- **2.9** Windows toast 通知接收方
- **2.10** 自有窗口替代浏览器(已落 v0.3.0 主体, **UI 待按 ADR-17 收尾**):WebView2 起 280×320 小窗加载 dashboard,删 Phase 1 临时打开浏览器代码。**UI 收尾**:换无边框、固定 ~280px 宽、只显示 **QR + 文件名 + 大小 + 关闭按钮**,不再加载 `/` 当前的完整页(里面有上传区块,违背 ADR-17)
- **2.11** 电脑端 / 手机端 UI 分化 + 模式选择(见 ADR-14 + ADR-17,**ADR-17 大幅修订此条**):
  - **路由设计**(ADR-17 修订版):
    - `/`  电脑端 dashboard。2.10 弹窗加载这里,**只渲 QR + 当前文件名/大小 + 关闭按钮**,无下载/上传/选文件区块
    - `/d` 手机端**发送目标页**(用户扫码进的下载页)。**用文件图标/缩略图 + 文件名 + 大小 + 下载按钮**, 没有 QR (手机不需要给自己看), 没有上传表单
    - `/u` 手机端**上传页**(接收模式专属,见 2.13)。仅上传表单, 没有下载链接, **只在接收模式开启时可达, 平时 404**
    - `/file` `/upload` `/qr` 保留为内部资源路由
  - **模式参数**:
    - `quickdrop send <path>` 或裸路径 → 发送模式,QR 编码 `http://<lan>:8443/d`
    - `quickdrop recv` 或托盘菜单点"接收文件" → 接收模式(见 2.13)
  - **无参 + 选"发送"时的文件对话框**(按 ADR-17, 这功能从弹窗剥离, 走配置页或独立入口):
    - 短期:HTML `<input type="file">` → 暂存 `os.TempDir()` → 由 `/d` 暴露(代价:文件先经一次"上传到自己")
    - 长期(配合 2.10):窗口内 JS 调 Win32 `GetOpenFileName` 或 File System Access API 拿磁盘原始路径
- **2.12** 托盘菜单扩充(配合 ADR-17 把复杂功能从弹窗剥离):"复制扫码链接"/"接收文件"/"打开配置"/"退出"。"接收文件"触发 2.13,"打开配置"开独立 webview 子窗加载配置页
- **2.13** 接收模式 + 安全分离(ADR-17 新增, 在 ADR-14 基础上加安全约束):
  - 默认 daemon 启动时 `/u` 路由 **不存在 (404)**,只在用户主动进入接收模式后才注册
  - 入口:`quickdrop recv` CLI / 托盘"接收文件"菜单 / 配置页按钮
  - 接收模式弹独立 webview 窗,只渲染一个 QR(指向 `/u`)+ "停止接收" 按钮
  - 接收完成(收到 N 个文件)或用户停止 → `/u` 路由立即注销 → 陌生人再扫旧 QR 一律 404
  - 解决当前隐患:任何陌生人扫到发送模式 QR 都能上传到 `~/Downloads/QuickDrop/`

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
| 15 | **WebView2 跑独立子进程**, 不和 systray 同进程 | systray 和 webview 都要求独占 main goroutine 跑 Windows 消息循环, 同进程会抢消息(webview issue #650, systray #195)。daemon fork `quickdrop window <url>` 子进程跑 webview, 用户关窗 → 子进程退出 → daemon 不受影响。天然契合"分享完即关"的体验, 且跨平台不打架 |
| 16 | **window-mode 三种策略 (replace/keep/first-only)** 由 env `QUICKDROP_WINDOW_MODE` 配置 | 一次 send 一个文件给一个人 → replace (默认, 屏幕始终 1 窗); 多文件多人 → keep (屏幕保留所有 QR 窗); 嫌烦 → first-only (只首次开窗)。Phase 2 后期配置页 UI 实装时把 env 移过去 |
| 17 | **极简 UI 修订** (完全推翻 ADR-13, 加强 ADR-14, 引入安全约束) | 实际用了一下发现 Phase 1/2.10 UI 长且没固定宽度、电脑端弹窗里居然有"上传"区块、手机扫码进的下载页又显示 QR——全是冗余。新方案: (1) **电脑端弹窗只显示 QR + 文件名 + 大小 + 关闭键**, 无边框, 固定 ~280px 宽; (2) **手机端发送页用文件图标/缩略图替代 QR** (手机不需要看自己的 QR); (3) **手机端默认无上传功能** — 任何陌生人扫到 QR 都能往你电脑塞文件是真实安全风险, 上传仅在用户主动进"接收模式"后才可用; (4) **复杂功能 (选文件 / 接收文件 / 配置)** 从弹窗剥离, 走托盘菜单或独立配置页, 弹窗保持极简 |
| 18 | **右键菜单走传统注册表 HKCU, Win11 顶级显示留 Phase 4** | 传统 `HKCU\Software\Classes\*\shell\X\command` 注册表方式: 简单 (~80 行 Go), 用户级无需 UAC, Win10 完美工作, Win11 显示在 "显示更多选项" / Shift+右键里。Win11 顶级菜单 (跟"复制""粘贴"同级) 需要 MSIX sparse package + IExplorerCommand COM, 工程量大且需打包安装器。先解决 90% 场景, MSIX 留 Phase 4 |
| 19 | **PC→PC 接收提示: Toast 主交互 + 托盘红点 fallback** | Windows 10/11 toast 原生支持 actions (最多 5 个按钮, "接受/拒绝"够用), 比"看托盘图标" 感知强一个数量级。但 toast 会被用户/系统设置静音 → 必须有 fallback 防丢消息: 同时改托盘 tooltip + 菜单加 "待处理 (N)" 红点项。Toast 按钮通过 `quickdrop://accept?id=xxx` URL scheme 触发 (install 时多注册一个 protocol handler), 接收方 daemon 解析 URL → IPC 完成接收。文件流: Pull 模式 (接收方拉发送方的 `/file?id=xxx`), 复用现有 server, 不引新对称协议 |
| 20 | **信任模型: 2.5 每次确认, 2.6 设备记忆后可信任** | 起步阶段 LAN 任何 QuickDrop 实例都能向你发 toast → 这是滥用面 (尤其公司/咖啡馆 WiFi)。2.5 阶段每次接收都要点 toast 按钮 (AirDrop 风格的强制确认), 即使是"自己的另一台电脑"。2.6 设备记忆做完后, toast 加 "信任此设备" 复选框, 勾上后该设备发文件免确认 (但 UI 仍有 toast, 只是默认 3 秒后自动接受)。永远保留 "永不信任此设备" 黑名单 |
