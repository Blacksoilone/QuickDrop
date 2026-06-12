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
                              路由含 /、/d、/r、/u、/file、/qr、/qr-recv、/upload、/api/info、/assets/*、/internal/*
internal/qr/qr.go             QR PNG 渲染
internal/tray/tray.go         systray, 菜单(复制链接/接收文件 checkbox/退出), UpdateTooltip + SetReceiveChecked
internal/tray/icon.ico        托盘图标 16x16 (Windows systray 必须 ICO,不能 PNG)
internal/window/window.go     webview 子进程实际渲染函数 (runWebview, 含 quickdropClose binding)
internal/window/manager.go    daemon 持有的子进程管理器 + Mode + recvCmd (发送窗+接收窗独立)
internal/installer/registry.go  Windows 右键菜单注册表读写 (HKCU 用户级,无需 UAC)
internal/installer/msgbox.go    Win32 MessageBoxW 包装 (windowsgui 模式下唯一可视反馈)
web/                          Vite + Vue3 + TS 前端工程
web/package.json              依赖 vue/vite/vue-tsc
web/vite.config.ts            MPA 4 入口 + dev proxy 转 :8443
web/index.html /d.html /r.html /u.html  4 个页面入口 (对应 server.go 4 路由)
web/src/pages/Dashboard.vue   电脑端发送窗 (拉 /api/info, 渲 QR + 名 + 大小 + 关闭)
web/src/pages/Download.vue    手机端发送目标页 (文件图标 + 信息 + 下载)
web/src/pages/Receive.vue     电脑端接收窗 (QR + 提示 + 停止接收)
web/src/pages/Upload.vue      手机端上传表单
web/src/api.ts                fetchInfo / stopReceiving / closeWindow 类型安全
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
npm run dev    # http://localhost:5173, /api+/qr+/file+/upload 自动 proxy 到 8443 daemon
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
  - `quickdrop recv` 新 CLI 子命令 / 托盘 "接收文件" checkbox 菜单项 (二者通过 IPC `/internal/receive on|off` 统一切换)
  - `server.EnableReceive(bool)` + `SetOnReceive` 回调: tray 菜单同步勾选 + winMgr 起/关接收窗
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
  - `web/` 完整 Vite + Vue3 + TypeScript MPA 工程, 4 个独立入口对应 4 路由
  - `web/src/pages/{Dashboard,Download,Receive,Upload}.vue` 替代原 HTML 模板字符串
  - `web/src/api.ts` 类型安全包装 fetchInfo / stopReceiving / closeWindow
  - server 新增 `/api/info` JSON API + `/assets/*` 静态资源 (来自嵌入的 dist)
  - `cmd/quickdrop` 用 `//go:embed all:web` 嵌入 Vite 产物 (build.ps1 自动从 web/dist 复制过去)
  - build.ps1 增加 Vue build 前置 (-SkipWeb 可跳过), npm 镜像换 npmmirror
  - 删除旧 internal/server/templates.go (268 行) 与所有 fmt.Fprintf HTML 拼接逻辑
  - 5 个回归测试脚本全 PASS (test-routes 改测 Vue 骨架 + /api/info; test-daemon-switch 改测 /api/info)
  - **开发体验飞跃**: `cd web && npm run dev` 起 5173, /api+/qr+/file+/upload 自动 proxy 到 8443 daemon, 热重载
- [x] **任务 2.5a mDNS 广播 + 发现** (ADR-19, ADR-20):
  - 新包 `internal/identity`: 持久化 UUID (~/.quickdrop/device-id) + 显示名 (env QUICKDROP_DEVICE_NAME / 主机名)
  - 新包 `internal/discovery`: 基于 grandcat/zeroconf, 服务名 `_quickdrop._tcp.local`, TXT 含 name/uuid/version
  - daemon 启动注册 + Browse, 退出注销; 过滤自身 UUID (自己看不到自己)
  - server 新增 /api/peers JSON 路由 (PeerSource 接口注入避免循环依赖)
  - QUICKDROP_PORT env 覆盖默认 8443 (probeDaemon 同步使用同一端口, 支持同机多 daemon 测试)
  - test-discovery.ps1 13 checks ALL PASS (mDNS 广播注册 / UUID 持久化跨重启 / 端口隔离 / 过滤自身)
  - **限制**: grandcat/zeroconf v1.0 默认不走 loopback (PR #68 待合), 同机两 daemon 互不可见; 真实 PC→PC 测试需要第二台 Windows PC

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

### 下一步 — 继续推 2.5b–e (PC→PC 互传剩余部分)

按 [QuickDrop.md §6 Phase 2.5](./QuickDrop.md):

- **2.5b** PC→PC IPC: `/peer/incoming` 接收元数据 + pending queue + `/peer/file?id=xxx` 鉴权下载
- **2.5c** Toast 通知 (ADR-19): `go-toast` 弹按钮 toast, install 注册 `quickdrop://` URL scheme
- **2.5d** `quickdrop accept --id` / `reject --id` 子命令: URL 触发 → Pull 文件
- **2.5e** 红点 fallback: 托盘菜单 "待处理 (N)" + `/pending` Vue 页面

完成后下一步是 **2.6 设备记忆 + 信任升级**: `~/.quickdrop/devices.json` + "信任此设备" 复选框.

### 后续 Phase 2 顺位

- **2.8 + 2.9** WebSocket 进度 + Windows toast 完成提示: 实时进度条 + 文件传输完成的轻通知
- **Phase 3 起步**: HTTPS via lancert.dev + PWA manifest, 真机适配开始

### 待补验收

- ⏸ TEST.md 用例 6 多手机并发(等借到第二台手机)
- ⏸ Win11 实地复核右键菜单显示位置 (Shift+右键 / "显示更多选项")
- ⏸ Vue 页面在真手机浏览器渲染 (UA 测试, 主要是 iOS Safari 兼容)
- ⏸ PC→PC 互传需要第二台 Windows PC (现阶段缺设备)

### 后续 Phase 2 才做的小项

- 托盘图标用绘图软件画一个像样的(目前是纯蓝 16x16 占位)
- daemon 模式发现端口被占但响应不是 QuickDrop 时,给用户更友好的报错
- `QUICKDROP_WINDOW_MODE` 配置项最终应进配置页 UI (现在只能 env 设置)
- 接收完成后自动关闭接收模式 (现在要用户手动点"停止接收"或托盘取消勾,有可能忘了一直开着)
- Win11 现代右键菜单 (顶级显示, 不需 Shift): 需要 MSIX sparse package + IExplorerCommand,工程量大,留 Phase 4
- 文件切换后 webview 旧窗内容不会自动刷新 (用户得 F5 / 重开窗才看到新文件名),等 2.8 WebSocket 解决
