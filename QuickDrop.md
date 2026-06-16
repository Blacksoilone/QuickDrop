# QuickDrop

> 本文档是产品蓝图。**写"要做成什么样"**，不写"进度到哪了"。
> 进度看 [PLAN.md](./PLAN.md)。

## 0. 一句话

Windows 上没有的 AirDrop —— 右键文件即发送，扫码即接收，局域网内 PC 互相自动发现。

## 1. 核心体验目标（量化"无感"）

- 右键文件 → 菜单"通过 QuickDrop 发送" → **1 秒内**弹窗
- 弹窗：二维码（手机扫）+ "发送到其他设备"按钮（局域网 PC 列表）
- 点目标后立即开始传输，传完自动关弹窗
- 接收方桌面右下角弹通知"小明的电脑想发 photo.jpg (2.3MB) [接收] [拒绝]"
- 手机端零安装，**系统相机扫码** → 浏览器即用
- **图标识别清晰** —— 托盘 / 任务栏 / 资源管理器都显示自定义图标

## 2. 明确不做的事

- ❌ 手机 ↔ 手机
- ❌ 公网穿透 / NAT 打洞 / 云中转
- ❌ 帐号系统 / 登录
- ❌ macOS、Linux 桌面集成（MVP 阶段）
- ❌ 文件夹镜像同步（只做"发送/接收"动作）
- ❌ 移动端原生 App（用扫码即可，不值得做）

## 3. 使用模式（双端角色不对等）

电脑是"决定者"，手机是"被动客户端"。一个 QR 对应一个动作，**手机端不存在同时能下载又上传的页面**。

| 模式 | 主入口 | 电脑端展示 | 手机端（扫 QR 后）|
|---|---|---|---|
| 发送 (PC → 手机) | **右键文件 → "通过 QuickDrop 发送"** 或 **托盘 "选文件发送..."** | 无边框小窗显当前文件 + QR | **下载页**（文件名 + 下载按钮，无上传入口）|
| 接收 (手机 → PC) | **托盘 "显示接收 QR 码"** 或 **设置页"常用 → 接收文件"开关** | 接收 QR 窗 | **上传页**（上传表单，无下载链接）|
| PC↔PC | **发送窗口"发送到其他设备"** → 选目标 PC | 弹 toast 通知对端 | 不涉及（局域网内自动发现）|

## 4. 运行形态（理想态）

- 程序常驻系统托盘，右下角一个小图标 — **平时不弹任何主窗口**
- **无边框窗口**，整个 QR 窗口可拖动（按住 QR 移动），符合 AirDrop 风格
- **双击 exe 默认启动 daemon**（不强求命令行参数）
- 右键发送是主交互，等同 Windows 用户分享文件的肌肉记忆；传完小窗自动消失，程序仍常驻等下次
- 点托盘图标 → "**设置**" 菜单项打开**配置中心**:
  - **常用**: 当前接收开关 + 常用配置项
  - **接收**: 保存目录（含浏览按钮）、冲突策略、文件大小上限、默认接收状态
  - **发送**: 发送相关行为（待充实）
  - **通知**: Toast 总开关 + 自动 reveal Explorer
  - **网络**: 端口、mDNS 广播开关
  - **设备**: 信任设备管理（搜索/筛选/排序/分组/重命名/删除）
  - **系统**: 开机自启 + 系统集成（注册右键菜单 / 取消注册）
  - **关于**: 版本号 + 配置文件路径

## 5. 技术栈（已定档，不再争论）

| 用途 | 选型 | 备注 |
|---|---|---|
| HTTP 服务器 | `net/http` 标准库 | 单聚焦工具，不上 Gin/Echo |
| WebSocket | `github.com/coder/websocket` | gorilla 已 archive，coder 是继任 |
| 二维码 | `github.com/skip2/go-qrcode` | PNG/SVG 输出 |
| 系统托盘 | `github.com/getlantern/systray` | Wails v2 没原生托盘，v3 alpha 风险高 |
| 自有窗口 | `github.com/webview/webview_go` + Windows WebView2 | 系统自带 Edge WebView2，不打包浏览器 |
| 无边框窗口 | Win32 API（`SetWindowLongPtrW` 移除 `WS_CAPTION`）+ `SendMessage(WM_NCLBUTTONDOWN, HTCAPTION)` 拖动 | webview 不支持 `-webkit-app-region: drag`，必须 Win32 |
| 文件/文件夹选择器 | Win32 API（`GetOpenFileNameW`、`SHBrowseForFolder`）| 原生体验 |
| mDNS 服务发现 | `github.com/grandcat/zeroconf` | 比 betamos 维护更活跃 |
| Windows toast 通知 | `github.com/go-toast/toast` | 原生 actions 支持（接受/拒绝按钮）|
| 进程同生共死 | Windows JobObject（`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`） | daemon 死时所有 webview 子进程自动死 |
| 前端框架 | Vue 3 + Vite | MPA 多入口（index/d/r/u/p/c）|
| UI 图标库 | `lucide-vue-next` | 现代线性图标，按需打包 |
| 前端打包 | Go 1.16+ `embed.FS` | 单二进制分发关键 |
| EXE 图标嵌入 | `github.com/akavel/rsrc` | 生成 `.syso` 资源文件 |

**禁用清单**:
- ❌ Wails（托盘问题）
- ❌ WebRTC（iOS Safari 在 LAN IP 上禁用安全上下文）
- ❌ Gin / Echo / Fiber（没必要）
- ❌ gorilla/websocket（已 archived）
- ❌ Cobra（MVP `flag` 包够了）
- ❌ Electron / Tauri（启动慢，体积大）

## 6. Phase 路线

### Phase 1 — 最小可跑通回路 ✅ 已完成

单文件 `main.go`，Windows 双击就能跑。

### Phase 2 — 系统集成 + Vue 前端 ✅ 已完成

- ✅ 拆包：`cmd/quickdrop/` + `internal/{server,qr,tray,window,...}`
- ✅ Daemon 模式 + IPC（HTTP `127.0.0.1:8443/internal/*`）
- ✅ Windows 右键菜单（HKCU 注册表）
- ✅ mDNS 发现 + PC↔PC 互传
- ✅ 设备记忆 + 信任升级（trust/ask/blocked）
- ✅ Vue 3 + Vite MPA + `embed.FS` 打包
- ✅ WebSocket 实时进度
- ✅ Windows toast 通知接收方
- ✅ WebView2 子进程
- ✅ 接收模式 + 安全分离（`/u` 路由 receiveMode 门禁）

### Phase 2.5 — UX 重构 ✅ 已完成

- ✅ 配置中心（`/c`，含 8+ 项可调配置）
- ✅ 设备管理合并到配置中心（删除独立 `/v` 页）
- ✅ Daemon 优雅退出（HTTP 异常 → 标准 cleanup）
- ✅ 子窗 OS 级同生共死（JobObject）
- ✅ 发送/接收解耦（接收状态由 config 决定，发送是动作）
- ✅ 选文件发送（托盘菜单 + Win32 文件选择器）
- ✅ 浏览文件夹（Win32 SHBrowseForFolder）
- ✅ 双击 exe 启动 daemon
- ✅ 无边框窗口（QR 窗整个可拖动 + 配置窗标题栏可拖动）
- ✅ 设备管理增强（搜索/筛选/排序/分组/别名/删除）
- ✅ Lucide 图标库
- ✅ EXE 图标嵌入（任务栏/任务管理器/资源管理器都显示）
- ✅ 系统注册（"注册到系统"取代"安装"用词）
- ✅ Linux 痕迹清理（路径/术语本地化）

### Phase 3 — 适配朋友机型（待启动）

- HTTPS via [lancert.dev](https://github.com/lucor/lancert) 通配证书，绑域名 `192-168-x-x.lancert.dev`
- PWA manifest + Service Worker（只缓存壳）
- iOS HEIC 浏览器端 `heic2any` 转 JPEG 再上传
- **微信内置浏览器适配**（中国市场最大坑）
- NoSleep.js 防熄屏
- 多机型 QA 矩阵

### Phase 4 — 体验打磨（待评估）

- **Rust Shell Extension**（详见 [docs/shell-extension-rust.md](./docs/shell-extension-rust.md)）—— 替换 CLI 右键方案，原生 IContextMenu，工程量大但产品级体验
- Win11 顶级右键菜单（MSIX sparse package）
- 自动更新
- 剪贴板共享

## 7. 决策记录（ADR）

为避免反复讨论已决事项，记录在此。要推翻请明说。

| ID | 决定 | 理由 |
|---|---|---|
| 1 | 不用 WebRTC | iOS Safari 在 LAN IP 上禁用安全上下文，LAN HTTP 反而吞吐更高 |
| 2 | 不用 Wails | v2 没托盘 API，v3 alpha 三年了 |
| 3 | HTTP 直连而非中继 | 服务器本身就是其中一端，中继毫无意义 |
| 4 | Phase 1 单文件 main.go | 不期望一开始就有合理结构，先跑通再分层 |
| 5 | Phase 1 不上 Vue | HTML 字符串够了，先验证后端骨架 |
| 6 | Windows 平台优先 | 目标用户也是 Windows 居多 |
| 7 | 仅 net/http 标准库 | 单聚焦工具不需要框架 |
| 8 | 二进制名 `quickdrop.exe` | 短、好记、符合命令行习惯 |
| 9 | 端口 8443 | 不冲突常见服务，数字像 https 端口暗示后期会上 TLS |
| 10 | 接收路径 `~/Downloads/QuickDrop/` | 跨平台都有 Downloads，自带子目录避免污染 |
| 11 | 最终 UI 必须是自有窗口，**不允许打开浏览器** | 浏览器有地址栏、标签页、需要找窗口，破坏"无感" |
| 12 | Phase 2 自有窗口方案：**WebView2** + 复用 `/qr` 路由 | Windows 10+ 系统自带 Edge WebView2，不需打包浏览器二进制 |
| 13 | **推翻 1.3"终端打印 QR"**，改为网页渲染 `/qr` PNG | 终端 QR 与最终形态路径相反，是死胡同 |
| 14 | **电脑端 / 手机端 UI 分化**：一个 QR 对应一个动作 | 用 URL 路径区分用途比 UA 嗅探稳，方便 Phase 3 PWA |
| 15 | **WebView2 跑独立子进程**，不和 systray 同进程 | systray 和 webview 都要求独占 main goroutine 跑 Windows 消息循环 |
| 16 | **window-mode 三种策略**（replace/keep/first-only），env `QUICKDROP_WINDOW_MODE` 配置 | 一次发一个文件给一个人 → replace；多文件多人 → keep；嫌烦 → first-only |
| 17 | **极简 UI 修订**（电脑端弹窗只显示 QR + 文件名 + 大小 + 关闭键，无边框）| 用了一下发现冗余多。手机端默认无上传功能（陌生人扫码塞文件是真实安全风险）|
| 18 | **右键菜单走传统注册表 HKCU**，Win11 顶级显示留 Phase 4 | 传统方式简单（~80 行 Go），用户级无需 UAC，覆盖 90% 场景 |
| 19 | **PC↔PC 接收提示**：Toast 主交互 + 托盘红点 fallback | Toast 原生支持 actions 但会被静音，必须 fallback 防丢消息 |
| 20 | **信任模型**：默认每次确认，可信任 / 拉黑设备 | 起步阶段 LAN 任何 QuickDrop 都能发 toast 是滥用面 |
| 21 | **JobObject 同生共死** | daemon 被强杀后 webview 子进程会成为孤儿（前台还在，后端 404）。用 Windows JobObject + `KILL_ON_JOB_CLOSE` 让 OS 一锅端 |
| 22 | **daemon 异常优雅退出** | 原 `log.Fatalf` 跳过所有 defer，导致 mDNS 不下播 + 子窗孤儿。改为 `log.Printf + close(httpDone)`，main goroutine 收到信号走标准 cleanup |
| 23 | **发送/接收解耦** | 原 `runDaemon(path, receiveMode bool)` 把发送/接收建模成模式互斥，导致 send 时强制关接收。改为：发送是动作，接收是状态由 config 决定 |
| 24 | **设备管理合并到配置中心** | 独立 `/v` 页面是冗余入口，全部移到配置中心 `/c#devices` |
| 25 | **删除独立 `/v` 路由** | 用户主入口是托盘"设置"菜单（不依赖 URL 书签），简化代码 |
| 26 | **无边框窗口默认开启，可关闭** | 现代设计要求；webview 不支持 `-webkit-app-region: drag`，必须 Win32 `SendMessage(WM_NCLBUTTONDOWN, HTCAPTION)` |
| 27 | **QR 窗口整窗可拖动**，配置窗只标题栏可拖动 | QR 窗追求极简体验，"任何非按钮处可拖动"；配置窗内容多，标题栏拖动避免误操作 |
| 28 | **设备别名** | 多台 Windows 都叫 LAPTOP-xxx，用户需要友好名 |
| 29 | **设备删除** | 防数据膨胀（测试设备/换 device-id 留下的旧条目）。删除后再次连接以"待决策"重新出现 |
| 30 | **Lucide 图标库** | 替代 Unicode/emoji，统一现代图标风格 |
| 31 | **不显示设备在线状态**（决议） | 暂时不做，等用户实际反馈再决定。增加复杂度但收益不明确 |
| 32 | **不区分手机设备类型**（决议） | 手机不安装软件、用扫码，不会出现在设备列表里 |
| 33 | **EXE 嵌入图标** | 用 `rsrc` 工具生成 `.syso` 资源文件，让任务栏/任务管理器/资源管理器显示自定义图标 |
| 34 | **"注册到系统" 取代 "安装"** | "安装"在 Windows 用户语境中容易混淆为"安装整个程序"，改用"注册"更准确 |
| 35 | **CLI 命令保留作为内部 IPC** | `send/recv/window/url-action` 命令不文档化，仅作为右键菜单/toast 等内部入口；用户只看 GUI |
