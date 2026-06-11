# QuickDrop 实现路线图

> 本文档由前期讨论汇总而成,作为 QuickDrop 的工程蓝图。
> 你负责写代码,我负责告诉你写什么、为什么、怎么验证。
> 任何任务里写"能问我"的问题,直接来问就行。

---

## 0. 产品定位

**一句话**: Windows 上没有的 AirDrop —— 右键文件即发送,扫码即接收,局域网内 PC 互相自动发现。

**核心体验目标**(用来量化"无感"):

- 右键文件 → 菜单选"通过 QuickDrop 发送" → **1 秒内**弹窗
- 弹窗里同时显示:
  1. 二维码(手机扫)
  2. 局域网内已发现的 PC 列表(点谁发谁)
- 点击发送目标后立即开始传输
- 传完自动关弹窗
- 接收方 PC 桌面右下角弹通知"小明的电脑想发 photo.jpg (2.3MB) [接收] [拒绝]"
- 手机端零安装,**系统相机扫码** → 浏览器即用

**明确不做的事**:

- ❌ 手机 ↔ 手机
- ❌ 公网穿透 / NAT 打洞 / 云中转
- ❌ 帐号系统 / 登录
- ❌ MVP 阶段不做 macOS、不做 Linux 桌面集成
- ❌ 文件夹镜像同步(只做"发送/接收"动作)

**使用模式(双端角色不对等,见 ADR-14 / ADR-15)**:

电脑是"决定者",手机是"被动客户端"。一个 QR 对应一个动作,**手机端不存在能同时下载又上传的页面**。

| 模式 | 主入口(理想态,见 ADR-15) | 电脑端展示 | 手机端(扫 QR 后) |
|---|---|---|---|
| 发送 (PC → 手机) | **右键文件 → "通过 QuickDrop 发送"** | 无边框小窗显当前文件 + QR | 仅**下载页**(文件名 + 下载按钮,无上传入口) |
| 接收 (手机 → PC) | **点托盘图标 → 配置页"接收文件"** | 小窗 / 配置页内显 QR | 仅**上传页**(上传表单,无下载链接) |

**运行形态(理想态,见 ADR-15)**:

- 程序常驻系统托盘,右下角一个小图标 — **平时不弹任何主窗口**
- 右键发送是主交互,等同 Windows 用户分享文件的肌肉记忆;传完小窗自动消失,程序仍常驻等下次
- 点托盘图标 → 一个很简单的**配置页**:
  - 参数项:端口、下载根目录、设备名、开机自启、接收前确认等
  - 按钮:"发送文件" / "接收文件"(等价于发起一次右键发送 / 进入接收模式)

**Phase 1 POC 阶段的简化(临时状态,与最终形态无关)**:

当前实现只验证后端骨架(HTTP / QR / 上传下载)能跑通,**不代表用户最终体验**:

- 不做托盘、不做右键菜单、不做 daemon
- 唯一入口是 `quickdrop.exe send <文件路径>`(拖拽到 .exe 等价)
- 启动一次跑一个 server,Ctrl+C 退出
- 完整理想态在 Phase 2 的 2.4(右键)+ 2.10(WebView2 小窗)+ 2.11(UI 分化)+ 2.12(托盘 + 配置页) 中分步实现

---

## 1. 开发环境约定

你在 WSL 里写代码,目标平台是 Windows。

| 环节 | 在哪 | 怎么做 |
|---|---|---|
| 写代码 | WSL Linux | Go toolchain、git、编辑器都在这 |
| 编译 | WSL | `GOOS=windows GOARCH=amd64 go build -o /mnt/c/Users/pangs/Desktop/quickdrop.exe` |
| 运行 | Windows 宿主机 | 在 PowerShell 里执行 `.\quickdrop.exe ...`,或双击 |
| 浏览器测试 | Windows 上的 Edge/Chrome | 直接 `http://localhost:8443`,**不要在 WSL 里测** |
| 手机测试 | 同 WiFi 的真手机 | 系统相机扫终端打印的 QR |

### 已知陷阱

- **WSL 的 `localhost` ≠ Windows 的 `localhost`**。所有 HTTP 测试都在 Windows 侧做。
- **Windows 防火墙** 第一次跑会弹窗问是否允许网络访问 —— 必须勾"专用网络"和"公用网络"全允许,否则手机连不上。
- **跨平台编译需要 `CGO_ENABLED=0`** 一开始足够了,等到 Phase 1.4 加 systray 时它依赖 CGO,需要在 Windows 上原生编译,或在 WSL 上装 mingw 交叉工具链。届时再处理。

---

## 2. 技术栈(已定档,不再争论)

| 用途 | 选型 | 备注 |
|---|---|---|
| HTTP 服务器 | `net/http` 标准库 | 不上 Gin/Echo,这是单聚焦工具 |
| WebSocket | `github.com/coder/websocket` | gorilla 已 archive,coder 是其继任 |
| 二维码 | `github.com/skip2/go-qrcode` | 终端打印 + PNG/SVG 输出 |
| 系统托盘 | `github.com/getlantern/systray` | Wails v2 没原生托盘,v3 alpha 风险高 |
| mDNS 服务发现 | `github.com/betamos/zeroconf` | Phase 2 才用,先不引入 |
| 前端框架 | Vue 3 + Vite | Phase 2 才用,Phase 1 只写 HTML 字符串 |
| 前端打包 | Go 1.16+ `embed.FS` | 单二进制分发的关键 |

**禁用清单**:
- ❌ Wails(托盘问题)
- ❌ WebRTC(iOS Safari 在 LAN IP 上禁用安全上下文)
- ❌ Gin / Echo / Fiber(没必要)
- ❌ gorilla/websocket(已 archived)
- ❌ Cobra(MVP 命令行参数 `flag` 包够了)

---

## 3. Phase 1 — 最小可跑通回路

**目标**: 写**一个** `main.go`,Windows 双击就能跑。**不分包**,所有代码在一个文件里,先跑通再说。

**Phase 1 验收**:
- 在 Windows 里 `.\quickdrop.exe send C:\Users\xxx\test.jpg`
- 终端打印 QR
- 手机扫 → 浏览器弹下载链接 → 文件到手
- 反向: 手机扫 QR 进网页 → 选文件上传 → 文件出现在 Windows `Downloads/QuickDrop/`

预期总代码量: **150-250 行 Go**。

### 任务 1.1 — HTTP 服务器骨架

**做什么**:
- `main.go` 启动一个 `http.Server` 监听 `0.0.0.0:8443`
- 注册路由 `/`,返回一段写死的 HTML(里面就一行 "QuickDrop 在跑,时间 = ...")
- 主函数最后用 `log.Fatal(srv.ListenAndServe())`

**不要**:
- 不要写 mux、中间件、handler 拆分
- 不要读配置文件、不要 dotenv
- 端口先写死 8443,**不**做 `--port` 参数

**自测**:
```powershell
# Windows PowerShell
.\quickdrop.exe
# 另开一个窗口
curl http://localhost:8443
# 应该看到 HTML
```

**手机测试**: 在 Windows 上 `ipconfig` 查 LAN IP(如 192.168.1.50),手机浏览器访问 `http://192.168.1.50:8443`。如果连不上,先关 Windows 防火墙试试,确认是防火墙问题再开回去并加白名单。

**能问我**:
- "Go 里怎么拿到本机所有 LAN IP?"
- "为什么监听 0.0.0.0 不监听 127.0.0.1?"
- "Windows 防火墙怎么自动加白名单?"

---

### 任务 1.2 — 路由 `/file`: 暴露一个写死的文件

**做什么**:
- 加一个 `/file` handler
- 内部读一个写死路径的文件(比如 `C:\Users\you\Desktop\test.jpg`)
- 用 `http.ServeFile` 输出
- 设置响应头 `Content-Disposition: attachment; filename="..."` 强制下载

**不要**:
- 不要做目录浏览、多文件、上传
- 不要做权限校验、token

**自测**: 手机访问 `http://<ip>:8443/file`,应该直接弹下载,而不是在浏览器里预览图片。

**能问我**:
- "Content-Disposition 的 filename 怎么处理中文?"
- "http.ServeFile 自动处理 Range 吗?(自动支持续传)"
- "怎么避免文件被多次读进内存?"

---

### 任务 1.3 — 终端打印二维码 (已修订, 见 ADR-13)

> **修订**: 不再走终端 `ToSmallString` 路线。QR 渲染为 PNG, 通过 `/qr` 路由暴露,
> 嵌进 `/` 主页 `<img>`。桌面端启动时 `cmd /c start` 自动开浏览器到主页。
> 网页 QR 是终端 QR 与最终 WebView2 小窗 (ADR-12) 之间的合理过渡形态。

**做什么**:
- 引入 `github.com/skip2/go-qrcode`
- 启动时构造下载 URL: `http://<本机 LAN IP>:8443/file`
- `qrcode.New(url, qrcode.Medium)`,然后 `ToSmallString(false)` 在终端打印

**不要**:
- 不要把 QR 渲染到网页上(Phase 2 才做)
- 不要把 QR 输出 PNG 文件
- LAN IP 直接选**第一个非回环、非 docker、非 WSL 虚拟网卡**的就行,先不做交互选择

**自测**: 终端能看到 QR,手机系统相机扫一下,识别出 URL 即成功。

**能问我**:
- "Go 里怎么过滤掉 docker0、vEthernet 这种虚拟网卡只留真 WiFi?"
- "QR 的 Medium / High 错误纠正等级有什么区别?"
- "为什么我打印出来的 QR 是横向拉长的?(终端字符纵横比问题)"

---

### 任务 1.4 — 系统托盘图标

**做什么**:
- 引入 `github.com/getlantern/systray`
- 启动时调 `systray.Run(onReady, onExit)`
- `onReady` 里:
  - `systray.SetIcon(iconBytes)` —— 用 `embed.FS` 嵌一个 16x16 的 PNG
  - 加菜单项 "复制扫码链接"、"退出"
- 点"退出"时停掉 HTTP server 然后 `systray.Quit()`

**不要**:
- 不要做菜单嵌套、动态菜单、图标切换
- 图标找一个临时的占位 PNG 就行

**关键陷阱**:
- `systray.Run` **会阻塞调用线程**,而且必须在 main goroutine 跑。HTTP server 要放到 `go func() {...}()` 里启动。
- 退出时 HTTP server 必须先 `Shutdown`,不然进程不会退干净。

**自测**: Windows 跑起来后,右下角任务栏应有图标。点"退出"程序应彻底退出(任务管理器看不到 quickdrop.exe)。

**能问我**:
- "systray 和 http server 共存的标准模式是什么?"
- "怎么优雅关闭一个正在传输文件的 HTTP server?"
- "systray 在 Linux 上要 GTK 依赖,WSL 编译时怎么交叉?"
- "把 PNG 图标用 embed.FS 嵌进去怎么写?"

---

### 任务 1.5 — 命令行: `quickdrop send <文件路径>`

**做什么**:
- 用 `flag` 包解析参数
- 第一个位置参数 `send` 触发"发送模式":启动 server + 暴露指定文件 + 打印 QR
- 没参数或参数错误 → 打印用法后 `os.Exit(1)`
- 路径处理: 用 `filepath.Abs` 转绝对路径

**不要**:
- 不要引入 cobra/urfave/cli
- 不做子命令树、不做配置文件、不做交互式选项

**预期形态**:
```powershell
# 用法
.\quickdrop.exe send C:\Users\you\Pictures\test.jpg
# 或者拖拽文件到 .exe 上(Windows 会传文件路径作为 args)
```

**自测**: 路径里带空格、带中文都要能跑。

**能问我**:
- "Windows 拖拽文件到 .exe 时,os.Args 是什么样的?"
- "中文路径在 Windows / Go 里要不要做编码转换?"
- "怎么处理用户传相对路径的情况?"

---

### 任务 1.6 — 反向: `/` 显示上传表单,`/upload` 接收

**做什么**:
- 改造 `/` handler: 返回一个含 `<form enctype="multipart/form-data">` 的 HTML
- 加 `/upload` handler:
  - 解析 `multipart/form-data`,**用流式 API**(`Request.MultipartReader()`),不要 `ParseMultipartForm`(后者会把整个文件读进内存)
  - 把上传的文件流式写到 `<用户Downloads>/QuickDrop/<原文件名>`
  - 返回简单的 HTML "上传成功"

**不要**:
- 不做进度条(Phase 2 用 WebSocket)
- 不做断点续传
- 不做并行分片
- 不做文件名冲突处理(同名直接覆盖,先跑通)

**自测**: 手机扫 QR → 网页里选一张照片 → 上传 → 在 Windows 的 Downloads/QuickDrop 找到文件。试试 100MB+ 的视频,确认内存不会爆。

**能问我**:
- "MultipartReader 和 ParseMultipartForm 的区别?"
- "Windows 用户的 Downloads 路径在 Go 里怎么拿?"
- "怎么避免一边写文件一边崩溃后留下半截文件?(临时文件 + rename)"

---

### 任务 1.7 — Phase 1 收尾

**做什么**:
- 跨平台编译命令写进 `Makefile` 或 `build.sh`
- 在 Windows 实地走完整流程 5 次,记录每个 bug
- 写一份 `TEST.md`,列出验收用例:
  1. 小图(<1MB)发送
  2. 大文件(>500MB)发送
  3. 中文文件名发送
  4. 中文文件名上传
  5. 传输中点托盘退出 → 程序应干净退出且不留临时文件
  6. 同时多个手机访问(行为 OK 即可,可能并发问题留 Phase 2)

**Phase 1 完成的标志**: 上面 6 条都过了,你能放心地把 .exe 给一个朋友说"扫码就能给我发文件"。

---

## 4. Phase 2 — 系统集成 + Vue 前端(粗规划)

> Phase 1 跑通后再展开细节。这里只列任务,不展开。

- **2.1** 拆包:`cmd/quickdrop/main.go` + `internal/server` + `internal/qr` + `internal/tray`
- **2.2** Daemon 模式: 第一次启动常驻;后续 `quickdrop send X` 检测到 daemon 已跑就走 IPC 通知,不重新启动进程
- **2.3** IPC: 用 HTTP 到 `127.0.0.1:8443/internal/send` 即可,简单粗暴。先不上 Named Pipe。
- **2.4** Windows 右键菜单: 写注册表 `HKCU\Software\Classes\*\shell\QuickDrop\command`,值是 `"C:\path\quickdrop.exe" send "%1"`。安装时写,卸载时删。
- **2.5** mDNS 广播 + 发现 PC 列表
- **2.6** 设备记忆: JSON 文件 `~/.quickdrop/devices.json` 记录已配对设备
- **2.7** Vue 3 工程化:
  - `web/` 目录,vite 构建
  - 构建产物 `web/dist/` 用 `embed.FS` 打进 .exe
  - 替换 Phase 1 写死的 HTML
- **2.8** WebSocket 实时进度
- **2.9** Windows toast 通知接收方
- **2.10** 自有窗口替代浏览器: 引入 `github.com/webview/webview_go`,起一个 280×320 无边框小窗,加载 `http://localhost:8443/qr`(Phase 1 临时打开浏览器的代码删除)。窗口顶部一行小字"扫码或选择设备",下方是 QR,再下方是已发现 PC 列表。点击 QR 之外区域 → 关窗。
- **2.11** 电脑端 / 手机端 UI 分化 + 模式选择(见 ADR-14):
  - **路由设计**:
    - `/`  电脑端 dashboard。无参启动时显示"发送 / 接收"选择;模式确定后显示当前 QR + 状态(进度、已连客户端等)
    - `/d` 手机端**下载页**。发送模式 QR 指向这里,仅渲染文件信息 + 下载按钮,**没有上传表单**
    - `/u` 手机端**上传页**。接收模式 QR 指向这里,仅渲染上传表单,**没有下载链接**
    - `/file` `/upload` `/qr` 保留作为内部资源路由
  - **模式参数**:
    - `quickdrop send <path>` 或裸路径 → 发送模式,QR 编码 `http://<lan>:8443/d`
    - `quickdrop recv` 或双击后选"接收" → 接收模式,QR 编码 `http://<lan>:8443/u`
  - **无参 + 选"发送"时的文件对话框**:
    - 短期方案:HTML `<input type="file">` 让浏览器选 → 把内容暂存到 server 内存 / `os.TempDir()` → 由 `/d` 暴露。代价:文件先经一次"上传到自己"。
    - 长期方案(配合 2.10 WebView2):窗口内 JS 调 Win32 `GetOpenFileName` 或 File System Access API,拿到磁盘原始路径,直接传给 server,免去临时拷贝
  - **Phase 1 `/` 路由迁移**:当前一份 HTML 同时含下载+上传,迁到 `/d`(去掉上传段)+ `/u`(去掉下载段)。`/` 改为电脑端模式选择 / dashboard

---

## 5. Phase 3 — 适配朋友机型(粗)

- HTTPS via [lancert.dev](https://github.com/lucor/lancert) 通配证书,绑域名 `192-168-x-x.lancert.dev`
- PWA manifest + Service Worker(只缓存壳)
- iOS HEIC 在浏览器端用 `heic2any` 转 JPEG 再上传
- **微信内置浏览器适配**(中国市场最大坑,需要单独投入)
- NoSleep.js 防熄屏
- 多机型 QA 矩阵

## 6. Phase 4 — 体验打磨(开放)

- Win11 顶级右键菜单(MSIX sparse package + IExplorerCommand,工程量较大)
- macOS Automator Service / Quick Action
- Linux Nautilus / Dolphin 集成
- 自动更新
- 剪贴板共享
- 多语言

---

## 7. 决策记录(ADR)

为避免反复讨论已决事项,记录在此。要推翻请明说。

| ID | 决定 | 理由 |
|---|---|---|
| 1 | 不用 WebRTC | iOS Safari 在 LAN IP 上禁用安全上下文,LAN HTTP 反而吞吐更高 |
| 2 | 不用 Wails | v2 没托盘 API,v3 alpha 三年了 |
| 3 | HTTP 直连而非中继 | 服务器本身就是其中一端,中继毫无意义 |
| 4 | Phase 1 单文件 main.go | 用户明确"不期望一开始就有合理结构",先跑通再分层 |
| 5 | Phase 1 不上 Vue | HTML 字符串够了,先验证后端骨架 |
| 6 | Windows 平台优先 | 用户在 WSL,目标用户也是 Windows 居多 |
| 7 | 仅 net/http 标准库 | 单聚焦工具不需要框架 |
| 8 | 二进制名 quickdrop.exe | 短、好记、符合命令行习惯 |
| 9 | 端口 8443 | 不冲突常见服务,数字像 https 端口暗示后期会上 TLS |
| 10 | 接收路径 ~/Downloads/QuickDrop/ | 跨平台都有 Downloads,自带子目录避免污染 |
| 11 | 最终 UI 必须是自有窗口,**不允许打开浏览器** | 打开浏览器破坏"无感"原则——浏览器有地址栏、标签页、需要找窗口。Phase 1 临时用浏览器,Phase 2 必改 |
| 12 | Phase 2 自有窗口方案: **WebView2 (Windows 自带)** + 复用 `/qr` 路由 | 不需打包浏览器二进制(Windows 10+ 系统自带 Edge WebView2),用 `github.com/webview/webview_go`,几行代码起一个无地址栏小窗加载 `http://localhost:8443/qr`。窗口大小固定,无边框无标题栏 |
| 13 | **推翻任务 1.3 的"终端打印 QR"**,改为网页渲染 `/qr` (PNG) + 主页内嵌 `<img>` | 终端 QR 与最终形态 (WebView2 小窗显示同一张 PNG) 路径相反: 终端打印是死胡同, 而网页 QR 直接复用同一资源, 也让手机扫码进主页能同时看到"下载文件"和"上传给电脑"两个动作 (一份页面服务两端) |
| 14 | **电脑端 / 手机端 UI 分化**: 手机端按 QR 编码的路径只看到一个动作(`/d` 下载 或 `/u` 上传),不存在同时能下载又上传的页面;电脑端独占"模式选择 / 选文件"职责 | 电脑是"决定者"(决定要发还是收、决定发哪个),手机是"被动客户端"(扫码即动作),一个 QR 对应一个动作可简化心智模型,也避免"扫错码后误操作"。用 URL 路径(而非 User-Agent 嗅探)区分用途,逻辑更稳,也方便 Phase 3 的 PWA 配置 — 注:此条**部分推翻 ADR-13** 的"一份页面服务两端",ADR-13 在 Phase 1 阶段仍成立,Phase 2 实现 2.11 时按本条拆分 |

---

## 8. 提问指南

**好的提问**(我能直接回答):
- "任务 1.4 里 systray 和 http server 怎么并存?给一个最小代码骨架"
- "Go 怎么拿到 Windows 用户的 Downloads 目录?"
- "我跑起来后手机扫码连不上,Windows 这边 netstat 能看到 :8443,但手机访问超时,可能是什么?"

**贴报错时请带**:
1. 完整报错文本(不要只贴一行)
2. 你写的相关代码片段
3. 你已经试过的尝试

**不那么有效的提问**:
- "帮我把任务 1.4 写完" —— 你想自己写,这违背初衷
- "我感觉这块怪怪的" —— 没法定位,先描述具体现象
- "这个项目怎么做" —— 看本文档

---

## 9. 当前状态

- [x] 决定项目定位:无感局域网传输
- [x] 选定技术栈
- [x] 画好 Phase 1-4 路线
- [x] 任务 1.1 HTTP 骨架(WSL 交叉编译 → `/mnt/c/Users/pangs/Desktop/quickdrop.exe`,Windows 原生运行,手机同 WiFi 已能访问)
- [x] 任务 1.2 /file 路由(`http.ServeFile` + `Content-Disposition: attachment` 强制下载已通)
- [x] 任务 1.3 网页 QR(已修订,ADR-13:`/qr` 返回 PNG,主页内嵌 `<img>`,启动自动开浏览器)
- [ ] 任务 1.4 系统托盘 ← **暂缓**(CGO 交叉编译陷阱,放到能在 Windows 原生编译或装 mingw 后再做)
- [x] 任务 1.5 命令行 send(`flag` 包,支持 `send <path>` 显式语法 + 拖拽单参数)
- [x] 任务 1.6 上传 /upload(`MultipartReader` 流式 + 临时文件 + rename,中文文件名 RFC 5987 编码)
- [ ] 任务 1.7 Phase 1 验收 ← **下一步**(等 1.4 决议后跑完整 6 项验收)

**Windows 用户名**: `pangs`(后续命令直接代入)
**编译命令(固化)**: `GOOS=windows GOARCH=amd64 go build -o /mnt/c/Users/pangs/Desktop/quickdrop.exe main.go`
