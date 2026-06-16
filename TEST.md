# QuickDrop 测试

> 本文档汇总自动化测试脚本 + 手工测试用例。

## 自动化测试 (PowerShell 脚本)

```powershell
# 准备测试文件 (一次性, 仓库自带 test.png 已可用)
.\test\prepare-fixtures.ps1
```

| 脚本 | 范围 | 预期 |
|---|---|---|
| `test\test-config.ps1` | 配置加载 / 保存 / env 优先级 / 默认值 | 24/24 PASS |
| `test\test-peer.ps1` | PC↔PC 互传 (mDNS + accept/reject + token 一次性) | 24/24 PASS |
| `test\test-receive.ps1` | 接收模式开关 / 上传 / 中文文件名 | ALL PASS |
| `test\test-discovery.ps1` | mDNS 广播 + 发现 | ALL PASS |
| `test\test-routes.ps1` | 路由分化 (Phase 2.11) | ALL PASS |
| `test\test-window.ps1` | webview 子进程管理 (replace/keep/first-only) | ALL PASS |
| `test\test-daemon-switch.ps1` | daemon 切换文件 IPC | ALL PASS |
| `test\test-pending.ps1` | pending dashboard | ALL PASS |
| `test\test-devices.ps1` | 设备信任表 + 黑名单 | ALL PASS |
| `test\test-toast.ps1` | toast 通知 + URL scheme | ALL PASS |
| `test\test-install.ps1` | 注册表注册 / 取消注册 | ALL PASS |
| `test\test-crash-cleanup.ps1` | 启动清扫残留 .tmp | PASS: directory clean |

**完整回归**: 把所有上述脚本跑一遍, 任何一个 FAIL 都需修复.

## 手工测试用例

构建后启动 daemon (双击 `quickdrop.exe` 或 `.\quickdrop.exe recv`):

### 用例 1: 小图发送 (PC → 手机)

```powershell
.\quickdrop.exe send test.png
```

- ☐ 托盘出现自定义图标 (青色圆形 + 白色右上箭头)
- ☐ 弹无边框 QR 窗 (264x294, 整窗可拖动)
- ☐ 文件名 `test.png` + 大小 `1.09 MiB` 显示
- ☐ 手机扫 QR → 浏览器打开 `/d` 下载页
- ☐ 手机点"下载" → 文件完整 (1,141,522 字节)
- ☐ 关闭窗后托盘仍存在

### 用例 2: 大文件 (>500MB) 发送

```powershell
.\quickdrop.exe send .\test\big.bin
```

- ☐ 文件大小显示 `500.0 MB`
- ☐ 手机端下载完成 (耐心等待)
- ☐ 传输过程中 quickdrop.exe 内存稳定 (不随传输线性增长)
- ☐ MD5 一致

### 用例 3: 中文文件名

```powershell
.\quickdrop.exe send .\test\你好世界.png
```

- ☐ QR 窗显示 `你好世界.png` (不乱码)
- ☐ 手机下载文件名也是 `你好世界.png` (RFC 5987)
- ☐ 手机端上传含中文文件 → Windows `~\Downloads\QuickDrop\` 出现正确文件名

### 用例 4: 多次 send (IPC 切换)

第一次 send 后 daemon 已驻留, 再来一次:

```powershell
.\quickdrop.exe send test.png
.\quickdrop.exe send .\test\big.bin
```

- ☐ 第二次不重启进程, 只切换文件
- ☐ QR 窗内容更新 (按 window-mode 行为)
- ☐ 客户端进程秒退

### 用例 5: 接收模式 (手机 → PC)

托盘点 "显示接收 QR 码":

- ☐ 弹接收窗 (无边框, 拖动可)
- ☐ 手机扫 → 上传页 (无下载链接)
- ☐ 手机选文件上传 → Windows `~\Downloads\QuickDrop\` 出现文件
- ☐ 关接收窗后接收**仍开启** (托盘"复制扫码链接"对应的接收 URL 仍可用)
- ☐ 配置中心"常用"页 toggle 关 → 上传 404

### 用例 6: PC↔PC 互传 (需第二台 Windows PC)

A 机 send 后, B 机扫到 A:

- ☐ A 弹 QR 窗显示当前文件
- ☐ A 点 "发送到其他设备" → 看到 B (mDNS 自动发现)
- ☐ 点 B → A 等响应, B 收 toast "X 想发 Y 文件"
- ☐ B 点 toast "接受" → A B 双方完成传输, B 收文件
- ☐ B 拒绝 → A 收 reject, 文件不传
- ☐ B 点击设备管理 → 信任 A → 下次自动接受

### 用例 7: 设备管理 UI

配置中心 → "设备" tab:

- ☐ 搜索框过滤设备 (按名称/UUID)
- ☐ 筛选下拉 (全部/信任/待决策/黑名单)
- ☐ 排序下拉 (最近活跃/名称)
- ☐ 黑名单组默认折叠, 点击展开
- ☐ ⋯ 菜单 → "重命名" → 行内编辑别名 → 回车保存
- ☐ ⋯ 菜单 → "拉黑/信任/每次询问" 切换状态
- ☐ ⋯ 菜单 → "删除" → 二次确认 → 设备消失

### 用例 8: 系统注册

配置中心 → "系统" tab → "系统集成":

- ☐ 状态徽章显示 "未注册" (默认)
- ☐ 点 "注册到系统" → 状态变 "已注册" + 提示成功
- ☐ 真去文件资源管理器右键文件 → 看到 "通过 QuickDrop 发送"
- ☐ 点 "取消注册" → 二次确认 → 右键菜单消失

### 用例 9: 配置热应用

配置中心改任意配置 (除 port 外) → 保存 → daemon 立即生效, 无需重启.

- ☐ download.dir 改后, 接收文件落到新目录
- ☐ download.conflict 改 overwrite, 重名文件覆盖
- ☐ ui.toasts_enabled 关, toast 不弹
- ☐ ui.borderless_windows 关 + 重启 daemon, QR 窗变带标题栏

### 用例 10: daemon 优雅退出

任务管理器强杀 quickdrop.exe:

- ☐ 所有 webview 子窗同步消失 (JobObject 生效)
- ☐ 不留 .tmp 残留 (启动时 cleanupStaleTmp)
- ☐ mDNS 不再广播 (其他设备的设备列表中消失, 30 秒内)

### 用例 11: 双击启动

无任何参数双击 quickdrop.exe:

- ☐ 不弹 QR 窗 (无文件)
- ☐ 托盘图标出现
- ☐ 不弹控制台窗口 (windowsgui 模式)
- ☐ 后续右键发送可用

## 已知阻塞 (设备不齐)

- ⏸ 多手机并发 (需 2 台手机)
- ⏸ Win11 顶级右键菜单 (Phase 4, 需 MSIX)
- ⏸ iOS Safari 渲染 (无 iOS 设备)
- ⏸ 第二台 Windows PC 测 PC↔PC

## 通过标准

自动化测试全 PASS + 手工用例 1-5 + 7-11 全部 ☐ 勾上 = 当前版本可发布.
用例 6 (需第二台 PC) 留待设备齐了补勾.
