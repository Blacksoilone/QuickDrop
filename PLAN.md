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
cmd/quickdrop/main.go         入口,window/recv/send/install/uninstall/status 子命令 + probeDaemon → client/daemon
                              + //go:embed all:web (Vite 产物嵌入二进制)
cmd/quickdrop/web/            (build 时由 build.ps1 从 web/dist 复制, 被 embed)
internal/server/server.go     HTTP server (Start/Shutdown/SwapFile/EnableReceive/SetDist),
                              路由含 /、/d、/r、/u、/p、/c、/file、/qr、/qr-recv、/upload、/api/*、/peer/*、/ws、/assets/*、/internal/*
internal/qr/qr.go             QR PNG 渲染
internal/tray/tray.go         systray, 菜单(复制扫码链接/打开接收窗口/发送到其他 PC/设置/退出), UpdateTooltip + pending badge
internal/tray/icon.ico        托盘图标 16x16 (Windows systray 必须 ICO,不能 PNG)
internal/window/window.go     webview 子进程实际渲染函数 (runWebview, 含 quickdropClose binding)
internal/window/manager.go    daemon 持有的子进程管理器 + Mode + 多类窗口 (发送/接收/待处理/配置)
internal/installer/registry.go  Windows 右键菜单注册表读写 (HKCU 用户级,无需 UAC)
internal/installer/msgbox.go    Win32 MessageBoxW 包装 (windowsgui 模式下唯一可视反馈)
web/                          Vite + Vue3 + TS 前端工程
web/package.json              依赖 vue/vite/vue-tsc
web/vite.config.ts            MPA 7 入口 + dev proxy 转 :8443
web/index.html /d.html /r.html /u.html /p.html /c.html /v.html  页面入口 (/v 为旧设备管理薄壳)
web/src/pages/Dashboard.vue   电脑端发送窗 (拉 /api/info, 渲 QR + 名 + 大小 + 关闭)
web/src/pages/Download.vue    手机端发送目标页 (文件图标 + 信息 + 下载)
web/src/pages/Receive.vue     电脑端接收窗 (QR + 提示 + 停止接收)
web/src/pages/Upload.vue      手机端上传表单
web/src/api.ts                fetchInfo / peers / devices / config / system / progress API 类型安全
web/src/style.css             全局基础样式 (色彩/字体/.btn/暗色模式)
build.ps1                     一键构建脚本 (Vue build → 复制 → Go build)
test/prepare-fixtures.ps1     生成 500MB big.bin + 你好世界.png 验收固件
test/test-crash-cleanup.ps1   验收用例 5 自动化 (中途崩溃 → 重启清理)
test/test-daemon-switch.ps1   Phase 2.2+2.3 验收: daemon 健康检查 / 客户端 IPC 切换 (改测 /api/info)
test/test-window.ps1          Phase 2.10 验收: webview 子进程 + 三种 window-mode
test/test-routes.ps1          Phase 2.11 + 2.7 UI 验收: Vue 骨架 + /api/info JSON
test/test-receive.ps1         Phase 2.13 验收: 接收模式独立入口 + /u 状态切换 + 上传落盘
test/test-install.ps1         Phase 2.4 验收: 注册表 install/uninstall/status + 幂等
TEST.md                       Phase 1 验收清单 (手动 + 自动)
```

**环境变量**:
- `QUICKDROP_WINDOW_MODE=replace|keep|first-only` 控制 daemon 弹窗策略,默认 replace

**前端开发**:
```powershell
cd web
npm run dev    # http://localhost:5173, /api+/internal+/peer+/ws+/qr+/file+/upload 自动 proxy 到 8443 daemon
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
- [x] **任务 2.1** 拆包:`cmd/quickdrop` + `internal/{server,qr,tray,window}`(见 §2)
- [x] **任务 2.2 + 2.3** Daemon + IPC:第二次 `quickdrop send Y` 不重启进程,POST `/internal/send` 让运行中的 daemon 切换文件。`/internal/*` 由 `requireLocal` 限制只 127.0.0.1 可访问,LAN 一律 404。tooltip 自动跟随当前文件。`test-daemon-switch.ps1` 自动 PASS
- [x] **任务 2.10** WebView2 小窗:daemon fork `quickdrop window <url>` 子进程,WebView2 280×320 固定大小加载主页 QR。删除"自动开浏览器"(ADR-11 真正落地)。子进程独立 main goroutine 避开 systray/webview 消息循环冲突。三种 window-mode(replace/keep/first-only)由 `QUICKDROP_WINDOW_MODE` env 控制,`test-window.ps1` 自动 PASS
- [x] **任务 2.11 + 2.10 UI 收尾** (ADR-17 极简 UI):
  - templates 拆三套:dashboardHTMLTpl(电脑端) / downloadHTMLTpl(手机 `/d`) / uploadHTMLTpl(`/u` 占位待 2.13)
  - `/` 极简 dashboard:只渲 QR + 文件名 + 大小 + 关闭按钮,固定 264×316 窗口
  - `/d` 手机端发送目标页:文件图标 SVG + 文件名 + 大小 + 下载按钮,**没有 QR**
  - `/qr` 编码改为 `mobileURL` (=`baseURL/d`),手机扫码进手机页
  - `/upload` 加 `receiveMode atomic.Bool` 门禁,默认 false → 404 (ADR-17 安全约束)
  - 托盘"复制扫码链接"复制 `mobileURL` (给朋友的链接)
  - 路由语义自动验证:`/`无上传无下载、`/d`无 QR 有下载、`/upload POST`默认 404、`/qr`仍 200 PNG
  - 像素采样验证窗口外观:QR 完整(851 黑像素)、无右侧滚动条(右边纯白)、尺寸贴合(279×353 含标题栏)
- [x] **任务 2.13 接收模式 + 安全分离** (ADR-17):
  - `quickdrop recv` 新 CLI 子命令 / 托盘接收入口 (二者通过 IPC `/internal/receive on|off` 统一切换)
  - `server.EnableReceive(bool)`: 切换 receiveMode 门禁, 接收窗口由 window manager 独立管理
  - 新路由: `/r` 接收 dashboard (QR + 提示 + 停止接收键) + `/qr-recv` 编码上传 URL + `/u` 上传表单 (受 receiveMode 门禁)
  - daemon 支持纯接收模式启动 (无文件): 发送类路由全 404 防泄露
  - 发送和接收可共存: send 模式 daemon 也可 IPC `recv on` 同时开两种模式
  - "停止接收"按钮: HTML `fetch /internal/receive off → quickdropClose()` 一键关接收+关窗
  - 接收窗 webview 子进程独立于发送窗 (Manager.recvCmd 单实例字段, 不受 window-mode 影响)
  - test-receive.ps1 17 checks ALL PASS (含 LAN 拦截 + 真实上传落盘验证)
- [x] **任务 2.4 Windows 右键菜单**:
  - `quickdrop install` / `uninstall` / `status` 三个新子命令
  - 注册到 `HKCU\Software\Classes\*\shell\QuickDrop` (用户级, 无需 UAC)
  - 菜单文字 "通过 QuickDrop 发送", 命令 `"<exe>" send "%1"`, 含空格路径安全 (双引号包)
  - Icon 字段指向 exe 的图标 0 (复用 systray icon 资源)
  - windowsgui 模式下用 Win32 MessageBoxW 给安装结果反馈; `-q` 静默给脚本测试用
  - test-install.ps1 18 checks ALL PASS (写入/幂等/状态/卸载/卸载幂等/路径含引号)
  - **真正的核心交互**: 右键文件 → "通过 QuickDrop 发送" → 1 秒内弹 QR 窗
- [x] **任务 2.7 Vue 工程化**:
  - `web/` 完整 Vite + Vue3 + TypeScript MPA 工程, 7 个独立入口对应当前页面路由
  - `web/src/pages/{Dashboard,Download,Receive,Upload,Pending,Config,Devices}.vue` 替代/承载各页面入口
  - `web/src/api.ts` 类型安全包装 fetchInfo / stopReceiving / closeWindow / peers / devices / config / system / progress
  - server 提供 `/api/*` JSON API + `/ws` 进度 + `/assets/*` 静态资源 (来自嵌入的 dist)
  - `cmd/quickdrop` 用 `//go:embed all:web` 嵌入 Vite 产物 (build.ps1 自动从 web/dist 复制过去)
  - build.ps1 增加 Vue build 前置 (-SkipWeb 可跳过), npm 镜像换 npmmirror
  - 删除旧 internal/server/templates.go (268 行) 与所有 fmt.Fprintf HTML 拼接逻辑
  - 5 个回归测试脚本全 PASS (test-routes 改测 Vue 骨架 + /api/info; test-daemon-switch 改测 /api/info)
  - **开发体验飞跃**: `cd web && npm run dev` 起 5173, /api+/internal+/peer+/ws+/qr+/file+/upload 自动 proxy 到 8443 daemon, 热重载
- [x] **任务 2.5a mDNS 广播 + 发现** (ADR-19, ADR-20):
  - 新包 `internal/identity`: 持久化 UUID (~/.quickdrop/device-id) + 显示名 (env QUICKDROP_DEVICE_NAME / 主机名)
  - 新包 `internal/discovery`: 基于 grandcat/zeroconf, 服务名 `_quickdrop._tcp.local`, TXT 含 name/uuid/version
  - daemon 启动注册 + Browse, 退出注销; 过滤自身 UUID (自己看不到自己)
  - server 新增 /api/peers JSON 路由 (PeerSource 接口注入避免循环依赖)
  - QUICKDROP_PORT env 覆盖默认 8443 (probeDaemon 同步使用同一端口, 支持同机多 daemon 测试)
  - test-discovery.ps1 13 checks ALL PASS (mDNS 广播注册 / UUID 持久化跨重启 / 端口隔离 / 过滤自身)
  - **限制**: grandcat/zeroconf v1.0 默认不走 loopback (PR #68 待合), 同机两 daemon 互不可见; 真实 PC→PC 测试需要第二台 Windows PC
- [x] **任务 2.5b PC↔PC IPC 协议** (ADR-17 安全约束):
  - 新包 `internal/peer`: Manager 持有 outgoing (Sender) + pending (Receiver) 两个状态机
    - Token 32 字符 hex (16 字节随机), 同时是 transferID + 鉴权
    - 一次性: Pull 成功后 MarkDelivered, 同 token 重 Pull 立即 404 (防重放)
    - 30 分钟未决策自动 expire, 5 分钟 GC delivered (防内存堆积)
  - server 新增 4 个 peer 路由:
    - POST `/peer/incoming` (来自其他 daemon): 入 pending queue
    - GET `/peer/file?token=xxx` (鉴权 Pull): token 匹配才 ServeFile, 否则 404
    - POST `/internal/peer-send` (Alice IPC): 创建 outgoing + POST 对端 /peer/incoming
      支持 toUUID (mDNS 查) 或 toIPv4+toPort (直连旁路, 测试用)
    - POST `/internal/peer-decide` (Bob IPC): accept → 异步 Pull, reject → 改状态
  - server 新增 GET `/api/pending` Vue 端拉待决策列表
  - test-peer.ps1 21 checks ALL PASS (双 daemon Alice→Bob 全流程: 邀请/接受/Pull/MD5 一致/token 一次性/reject)
- [x] **任务 2.5c Toast 通知 + URL scheme** (ADR-19):
  - 新包 `internal/notify`: 基于 go-toast, 弹原生 Windows 10/11 toast 含 [接受] [拒绝] 按钮
  - install 同时注册 `HKCU\Software\Classes\quickdrop` URL scheme (新 internal/installer/registry.go::installURLScheme)
    - quickdrop://accept?token=xxx / quickdrop://reject?token=xxx → 启动 `quickdrop.exe url-action "<url>"`
  - main.go 新增 `url-action` 子命令: 解析 URL 后 POST /internal/peer-decide
  - server.SetOnPeerIncoming 回调: handlePeerIncoming 收到后异步触发 notify.Incoming 弹 toast
  - uninstall 同步清除 URL scheme 4 个子键 (scheme/shell/open/command 链)
  - test-toast.ps1 16 checks ALL PASS (URL scheme 注册/卸载/url-action CLI 调 daemon 全流程)
  - **限制**: 实际 toast 弹出需肉眼验证 (无法自动截图判别 Action Center 内容), 调用链已通过编译 + url-action IPC 已验证
- [x] **任务 2.5e 红点 fallback** (Phase 2.5 完结, ADR-19 兜底):
  - tray 加 `SetPendingCount(n)` 同步 tooltip + 菜单项 + 图标三处:
    - tooltip: 当前文件名后追加 "N 个待处理"
    - 菜单项: "待处理 (N)" 默认隐藏, n>0 显示并改文字
    - 图标: 切到 icon-alert.ico (蓝底右上角小红方块)
  - server 加 `SetOnPendingChange` 回调, AddPending/SetPendingState/gcLoop 都触发
  - peer.Manager 加 `SetOnChange` 回调机制 (gc 过期 expire 时也通知)
  - 新路由 `/p` 服务 Vue pending dashboard, 不受 receiveMode 门禁
  - 新 Vue 页 `web/src/pages/Pending.vue`: 列出所有 incoming, 每条 [接受][拒绝], 2 秒轮询刷新
  - main.go 接 tray 的 onPending 回调 → winMgr.OpenPendingWindow(baseURL + "/p") 起子进程
  - window.Manager 加 `OpenPendingWindow` 单实例字段 pendCmd
  - test-pending.ps1 12 checks ALL PASS: /p 可达 + pending count 变化 + reject 状态变化
  - **托盘红点 + tooltip 切换 + 菜单显隐是 GUI 行为, 需肉眼验证**
- [x] **任务 2.6 设备记忆 + 信任白名单** (ADR-20 完整落地):
  - 新包 `internal/devices`: Store 持久化到 ~/.quickdrop/devices.json,
    原子写 (临时文件 + rename), 进程崩溃不留半截 JSON
  - 三档信任: ask (默认弹 toast 按钮) / trusted (静默自动接受 + 纯通知 toast) / blocked (静默拒绝)
  - server.handlePeerIncoming 按 trust 分支:
    - blocked → 不入 pending, 不弹 toast (但 UpsertSeen 还记 lastSeen 方便管理页看)
    - trusted → 立刻 AddPending + SetPendingState("accepted") + 启 Pull + 纯通知 toast
    - ask → 走 2.5c 原路径 (弹按钮 toast 等用户)
  - 新 API:
    - GET `/api/devices` 列所有已知设备 (按 lastSeen 倒序)
    - POST `/internal/device-trust` { uuid, name?, trust } 设/撤
    - GET `/api/pending` 每条 join trust 字段给 Vue 显示徽章
    - POST `/internal/peer-decide` 加可选 `trust` 参数 (accept→trusted / reject→blocked 一气呵成)
  - 新 Vue 页:
    - Pending.vue 每条加 checkbox "信任此设备"; 接受时同时设 trusted, 拒绝时同时设 blocked
    - Devices.vue (新, /v 路由) 列所有已知设备, 每条 [每次问][信任][黑名单] 三按钮直接切
  - 新 notify.IncomingSilent: 信任设备自动接受时弹纯通知 toast (无按钮)
  - test-devices.ps1 24 checks ALL PASS:
    UpsertSeen / 设 trust 持久化 / trusted 立即 accept / blocked 不入 pending / 重启不丢
  - **托盘没加 "设备管理" 菜单项**, 用户暂时手动访问 `http://127.0.0.1:8443/v`
    或者从 Pending 页加链接进入 (后续小修)

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


## 5. 当前状态总结

### 已完成 (按版本)

#### v0.10.x — Phase 2 收尾
- ✅ 托盘"设备管理"菜单 + 通知 + WebSocket 进度

#### v0.11.0 — 配置中心
- ✅ `internal/config` 包 (8 字段, 三层加载: 默认 < JSON < env)
- ✅ 8 项可调配置: download.dir / conflict / port / mdns / max_file_size / toast / reveal / autostart
- ✅ 配置中心页面 (`/c`, 现代化 UI, 960×640)
- ✅ 设备管理合并到配置中心 (废弃独立 `/v` 页)
- ✅ 托盘"设置"菜单入口

#### v0.11.1 — 孤儿 webview 修复 (JobObject)
- ✅ `internal/window/job_windows.go` Win32 JobObject + KILL_ON_JOB_CLOSE
- ✅ daemon 死时 OS 自动 SIGKILL 所有 webview 子进程

#### v0.11.2 — daemon 优雅退出
- ✅ `log.Fatalf` → `log.Printf + close(httpDone)` + tray.Quit
- ✅ runDaemon 全局 `defer recover` 兜底主 goroutine panic
- ✅ 修复 listener 异常时 daemon 跳过 cleanup 的 bug

#### v0.11.3 — 发送/接收解耦 (架构重构)
- ✅ 发送 = 离散动作, 接收 = 持续状态 (config 驱动)
- ✅ `internal/transfer.Manager` 管理发送队列 (预留多文件)
- ✅ `internal/receive.Manager` 管理接收状态 + 策略 (预留 auto_accept_trusted)
- ✅ `runDaemon(initialPath string)` 删 `initialReceive` 参数
- ✅ `Server.New(port, *receive.Config)` 不强求初始文件路径
- ✅ `quickdrop recv` 通过 env `QUICKDROP_FORCE_RECEIVE=1` 强制开启
- ✅ config 加 `receive.default_on` (默认 true)
- ✅ 删除托盘"接收文件" checkbox (改由配置中心常用页控制)
- ✅ 删除独立 `/v` 路由 (设备管理只走 `/c#devices`)
- ✅ 修复 webview 子窗 → /internal/* 404 (改用 LocalURL 127.0.0.1)

#### v0.12.0 — 用户体验全面打磨
- ✅ "选文件发送..." 托盘菜单 + Win32 GetOpenFileNameW
- ✅ "显示接收 QR 码" 托盘菜单
- ✅ 双击 exe 默认启动 daemon (无参 = runDaemon(""))
- ✅ "复制扫码链接" 反馈 (Toast 组件 + 复制成功提示)
- ✅ 文件夹选择器 (Win32 SHBrowseForFolder + /internal/pick-folder API)
- ✅ "注册到系统" 取代 "安装" (UI 用词改进)
- ✅ 系统集成 API (/internal/system-{register,unregister,status})
- ✅ 配置页"系统"区域: 显示注册状态 + 注册/取消注册按钮
- ✅ Linux 痕迹清理 (~/路径 → C:\Users\..., daemon → 后台服务)
- ✅ 托盘菜单简化 (合并设备管理到设置, 去掉冗余项)
- ✅ 接收开关重设计:
  - 配置中心"常用"页: 当前接收状态 toggle (运行时, 立即生效)
  - 配置中心"接收"页: 默认接收状态 toggle (启动初值, 二次确认)

#### v0.12.x — 设备管理增强 + 无边框窗口 + 图标
- ✅ Lucide 图标库 (lucide-vue-next, 替代所有 emoji/Unicode)
- ✅ 设备管理重写 (DevicePanel.vue 完全重写):
  - 搜索栏 (按名称/别名/UUID)
  - 筛选下拉 (全部/信任/待决策/黑名单)
  - 排序下拉 (最近活跃/名称)
  - 分组展示 (信任/待决策/黑名单, 黑名单默认折叠)
  - 设备别名 (行内编辑, 回车保存)
  - 删除设备 (二次确认弹窗)
  - 操作菜单 (⋯ 按钮展开)
- ✅ 后端: `Device.Alias` 字段 + `SetAlias` / `Delete` 方法
- ✅ 后端 API: `/internal/device-alias` + `/internal/device-delete`
- ✅ 无边框窗口:
  - Win32 API 移除 WS_CAPTION (`MakeBorderless`)
  - JS bind 调 Win32 SendMessage(WM_NCLBUTTONDOWN, HTCAPTION) 实现拖动
  - 配置项 `ui.borderless_windows` (默认 true)
  - QR 窗口 (Dashboard/Receive): 整窗可拖动 + 浮动关闭按钮 (MiniShell 组件)
  - 配置窗口 (Config): 自定义标题栏可拖动
  - 修复关键 bug: spawnSizedWithOptions 在 width=0 时跳过 size 参数,
    导致 borderless 标志位错位 → 改为扫描全部参数
- ✅ EXE 嵌入图标 (rsrc + .syso):
  - assets/icon-source.svg 源文件
  - cmd/quickdrop/icon.syso (rsrc 生成)
  - 任务栏 / 任务管理器 / 资源管理器 都显示自定义图标
- ✅ 替换托盘图标 (favicon.ico → internal/tray/icon.ico)

### 进行中

无 (准备 commit + tag v0.13.0)

### 待启动

#### Phase 3 — 适配朋友机型
- HTTPS via lancert.dev 通配证书
- PWA manifest + Service Worker
- iOS HEIC 转换
- 微信内置浏览器适配

#### Phase 4 — 体验打磨 (待评估)
- **Rust Shell Extension** (详见 [docs/shell-extension-rust.md](./docs/shell-extension-rust.md))
  - 替换 CLI 右键 (~50ms fork) → 原生 IContextMenu (<5ms)
  - 工程量: ~1 周, 但产品级体验
- 自动更新
- 剪贴板共享

## 6. 配置项扩展池 (用户暂缓, 后续按需挑)

第一轮已交付: download.dir / download.conflict / server.port / server.mdns_enabled /
receive.max_file_size / receive.default_on / ui.toasts_enabled / ui.reveal_on_done /
ui.borderless_windows / system.autostart.

下面这些是用户审过但说"暂时不做"的候选, 各自语义已讨论过:

- **receive.ask_before_accept** — 接收前是否弹确认窗 (默认 false: 当前直接 toast)
- **receive.max_pending** — 接收数量上限 (防 toast 轰炸)
- **receive.pending_ttl_sec** — pending TTL 可配 (现在硬编码 30 min)
- **ui.window.send_auto_close_sec** — 发送窗自动关闭 (现在常驻)
- **ui.window.width / height / dpi** — 窗口尺寸 / DPI 偏好
- **log.path / log.level** — 日志路径 / 级别
- **net.https_cert_path / key_path** — HTTPS 证书路径 (Phase 3)
- **ui.hotkey.global** — 全局热键 (Ctrl+Alt+Q 等)
- **system.post_receive_hook** — 接收完成自定义命令钩子
- **identity.broadcast_name** — 设备显示名独立于 hostname
- **ui.window_mode** — 当前是 env QUICKDROP_WINDOW_MODE 应进 config

加入时机: 用户主动提"加 X"再做. config.json 已支持 partial body 解析, 加字段无破坏性升级.

## 7. 待补验收

- ⏸ TEST.md 用例 6 多手机并发 (等借到第二台手机)
- ⏸ Win11 实地复核右键菜单显示位置 (Shift+右键 / "显示更多选项")
- ⏸ Vue 页面在真手机浏览器渲染 (UA 测试, 主要是 iOS Safari 兼容)
- ⏸ PC↔PC 互传需要第二台 Windows PC

## 8. 已知小项 (低优先级)

- daemon 模式发现端口被占但响应不是 QuickDrop 时, 给用户更友好的报错
- 文件切换后 webview 旧窗内容不会自动刷新 (考虑用 WebSocket 主动推送)
- 设置页"发送"分组当前为空 (待设计配置项)
- 红点告警图标和普通图标当前用同一个 (favicon.ico), 后续可做带红点变体
