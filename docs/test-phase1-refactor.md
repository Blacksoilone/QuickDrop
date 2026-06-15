# Phase 1 重构验收测试文档

> **重构目标**：发送/接收解耦 — 接收状态由配置决定，不由启动方式决定。
> **核心修复**：`quickdrop send` 时接收不再被强制关闭。

---

## 1. 编译验证

### 1.1 完整编译

```powershell
.\build.ps1 -SkipWeb
```

**预期输出**：
```
=== 跳过 Vue build (-SkipWeb) ===

=== Go build [RELEASE (windowsgui)] ===
  go: go version go1.26.0 windows/amd64
  gcc: gcc.exe (x86_64-posix-seh-rev1, Built by MinGW-Builds project) 16.1.0
BUILD OK: quickdrop.exe 22.5 MB
```

**验收标准**：
- [ ] 编译成功（exit code 0）
- [ ] quickdrop.exe 生成（~22-23 MB）
- [ ] 无 error 或 warning

---

## 2. 自动化回归测试

### 2.1 test-peer（核心功能）

```powershell
.\test\test-peer.ps1
```

**预期输出**：
```
ALL PASS
```

**验收标准**：
- [ ] 24/24 PASS
- [ ] Alice/Bob peer 互传成功
- [ ] accept/reject 流程正常
- [ ] token 一次性消费正常

**如果失败**：
- 截图错误信息
- 查看日志：`Get-Content $env:TEMP\quickdrop.log -Tail 50`
- 记录哪一步 FAIL

---

### 2.2 test-config（配置加载）

```powershell
.\test\test-config.ps1
```

**预期输出**：
```
ALL PASS
```

**验收标准**：
- [ ] 24/24 PASS
- [ ] config.json 读写正常
- [ ] env 覆盖生效
- [ ] 默认值兜底正常

---

## 3. Phase 1 核心功能验证（需手机 + 第二台 PC）

### 3.1 默认行为：接收默认开启

**步骤**：
1. 确保 `~/.quickdrop/config.json` 不存在（或删掉 `receive` 字段）
   ```powershell
   Remove-Item ~\.quickdrop\config.json -ErrorAction SilentlyContinue
   ```

2. 启动 daemon（recv 模式）
   ```powershell
   .\quickdrop.exe recv
   ```

3. 查看日志
   ```powershell
   Get-Content $env:TEMP\quickdrop.log -Tail 20
   ```

**预期日志包含**：
```
接收模式: → true
HTTP 启动: http://192.168.x.x:8443
```

**验收标准**：
- [ ] 托盘图标出现
- [ ] 右键托盘 → "接收文件" checkbox **已勾选**
- [ ] 日志显示 "接收模式: → true"

---

### 3.2 配置覆盖：接收默认关闭

**步骤**：
1. 手动编辑 config.json
   ```powershell
   $cfg = @"
   {
     "receive": {
       "default_on": false
     }
   }
   "@
   [System.IO.File]::WriteAllText("$HOME\.quickdrop\config.json", $cfg, [System.Text.Encoding]::UTF8)
   ```

2. 重启 daemon
   ```powershell
   Get-Process quickdrop -ErrorAction SilentlyContinue | Stop-Process -Force
   Start-Sleep -Seconds 1
   .\quickdrop.exe recv
   ```

3. 查看日志
   ```powershell
   Get-Content $env:TEMP\quickdrop.log -Tail 20
   ```

**预期日志包含**：
```
config: ... maxSize=0 ... (包含配置摘要)
接收模式: → false
```

**验收标准**：
- [ ] 托盘 "接收文件" checkbox **未勾选**
- [ ] 日志显示 "接收模式: → false"（或无此行，因为初值就是 false）

---

### 3.3 核心验证：send 时接收不被关闭（⭐ Phase 1 目标）

**前置条件**：
- 一台 PC（主机）
- 一部手机（在同一 WiFi）

**步骤**：

1. **主机：启动 daemon（接收默认开启）**
   ```powershell
   # 清理旧配置，确保 default_on=true
   Remove-Item ~\.quickdrop\config.json -ErrorAction SilentlyContinue
   .\quickdrop.exe recv
   ```

2. **主机：验证接收已开启**
   - 托盘右键 → "接收文件" 应**勾选**
   - 日志：`Get-Content $env:TEMP\quickdrop.log -Tail 10`
   - 应有：`接收模式: → true`

3. **主机：发送一个文件（不关 daemon）**
   ```powershell
   # 新开一个终端窗口
   .\quickdrop.exe send test.png
   ```

4. **⭐ 关键验证：接收状态不变**
   - 查看托盘 → "接收文件" checkbox 应**仍然勾选**（v0.11 会变成未勾选）
   - 日志中**不应有** `接收模式: true → false`

5. **手机：同时上传文件到主机**
   - 手机浏览器打开：`http://192.168.x.x:8443/u`（从日志找 IP）
   - 选一个文件 → 点"上传"

6. **主机：验证上传成功**
   - `~\Downloads\QuickDrop\` 应出现手机上传的文件
   - 日志应有：`保存上传文件: xxx.jpg`

**预期结果**：
- [ ] send 后接收 checkbox **仍勾选**（v0.11 会变成未勾选 ❌）
- [ ] 手机上传**成功**（v0.11 会 404 ❌）
- [ ] 日志无 `接收模式: true → false`

**如果失败**（接收被关）：
- 截图托盘菜单状态
- 贴日志中所有 `接收模式:` 开头的行
- 说明：Phase 1 重构失败，send 仍在关接收

---

### 3.4 recv 命令强制开启（即使配置默认关）

**步骤**：

1. **配置接收默认关闭**
   ```powershell
   $cfg = @"
   {
     "receive": {
       "default_on": false
     }
   }
   "@
   [System.IO.File]::WriteAllText("$HOME\.quickdrop\config.json", $cfg, [System.Text.Encoding]::UTF8)
   ```

2. **启动 daemon（recv 模式）**
   ```powershell
   Get-Process quickdrop -ErrorAction SilentlyContinue | Stop-Process -Force
   Start-Sleep -Seconds 1
   .\quickdrop.exe recv
   ```

3. **验证接收已强制开启**
   - 托盘 "接收文件" 应**勾选**（即使 config 说 default_on=false）
   - 日志应有：`接收模式: → true`

**预期结果**：
- [ ] config default_on=false
- [ ] 但 `quickdrop recv` 强制开启接收
- [ ] 托盘 checkbox 勾选

**设计意图**：
- `recv` 命令明确表达"我要接收"
- 即使用户配置了默认关闭（安全模式），显式 `recv` 也应开启

---

## 4. PC↔PC 互传（需第二台 Windows PC）

### 4.1 两台 PC 同时发送 + 接收

**设备**：
- PC-A（主机）
- PC-B（另一台 Windows）

**步骤**：

1. **PC-A：启动 daemon + 发送文件**
   ```powershell
   .\quickdrop.exe send fileA.txt
   ```

2. **PC-B：启动 daemon + 发送文件**
   ```powershell
   .\quickdrop.exe send fileB.txt
   ```

3. **PC-A：验证接收仍开启**
   - 托盘 "接收文件" 应**勾选**

4. **PC-A：从 Vue dashboard 发送给 PC-B**
   - 浏览器打开 `http://localhost:8443/`
   - 看到 "发现的设备" 列表中有 PC-B
   - 点 "发送到 PC-B"

5. **PC-B：接收 toast 弹出 → 点"接受"**

6. **PC-B：同时发送给 PC-A**
   - PC-B 浏览器 `http://localhost:8443/`
   - 点 "发送到 PC-A"

7. **PC-A：接收 toast → 点"接受"**

**预期结果**：
- [ ] PC-A 和 PC-B 同时发送 + 接收（双向）
- [ ] fileA.txt 到达 PC-B 的 `~/Downloads/QuickDrop/`
- [ ] fileB.txt 到达 PC-A 的 `~/Downloads/QuickDrop/`
- [ ] 两台 PC 的接收 checkbox **全程勾选**

**如果失败**：
- 检查哪一步卡住
- 贴两台 PC 的日志（grep `peer-send|incoming|接收模式`）

---

## 5. 边界情况测试

### 5.1 快速切换 send/recv

**步骤**：
```powershell
.\quickdrop.exe recv
Start-Sleep -Seconds 2
.\quickdrop.exe send fileA.txt
Start-Sleep -Seconds 2
.\quickdrop.exe send fileB.txt
Start-Sleep -Seconds 2
Get-Content $env:TEMP\quickdrop.log -Tail 30
```

**预期**：
- [ ] 每次 send 只更新发送文件
- [ ] 接收状态**不变**
- [ ] 日志只有一次 `接收模式: → true`（recv 时）
- [ ] 之后 send 无 `接收模式:` 日志

---

### 5.2 托盘手动切换接收

**步骤**：
1. daemon 运行中
2. 托盘右键 → 取消勾选 "接收文件"
3. 等 2 秒
4. 再勾选
5. 查看日志

**预期日志**：
```
接收模式: → false
接收模式: → true
```

**验收标准**：
- [ ] 手动切换生效
- [ ] send 不会覆盖手动选择

---

## 6. 回归检查清单

Phase 1 不应破坏现有功能：

- [ ] 右键菜单 "通过 QuickDrop 发送" 仍可用
- [ ] 托盘 "选文件发送..." 仍可用
- [ ] 手机扫码下载文件正常
- [ ] 手机上传文件正常（接收开启时）
- [ ] PC↔PC peer-send 正常
- [ ] toast 通知正常
- [ ] config.json 热更新生效
- [ ] mDNS 发现正常

---

## 7. 日志分析要点

### 关键日志行

**启动时**：
```
config: port=8443 dl="..." conflict=rename maxSize=0 toast=true reveal=true mdns=true autostart=false
接收模式: → true  (或 false, 取决于 default_on)
HTTP 启动: http://192.168.x.x:8443
```

**send 时（Phase 1 核心）**：
```
切换发送文件: D:\path\to\file.txt (12345 bytes)
```
**不应有**：`接收模式: true → false`

**手动切换接收时**：
```
接收模式: → false
接收模式: → true
```

**手机上传时**：
```
POST /upload (接收开启时)
保存上传文件: filename.jpg (12345 bytes)
```
或：
```
404 /upload (接收关闭时，符合预期)
```

---

## 8. 常见问题排查

### 问题 1：编译失败

**症状**：
```
cannot use ... as ... in argument
```

**排查**：
- 检查 `internal/transfer/manager.go` 是否存在
- 检查 `internal/receive/manager.go` 是否存在
- 检查 `cmd/quickdrop/main.go` import 是否加了 `quickdrop/internal/receive`

---

### 问题 2：send 时接收仍被关闭

**症状**：
- 执行 `quickdrop send` 后托盘 "接收文件" 变成未勾选
- 手机上传 404

**排查**：
```powershell
Get-Content $env:TEMP\quickdrop.log | Select-String "接收模式"
```

**应该看到**：
```
接收模式: → true  (只在 recv 或启动时)
```

**不应该看到**：
```
接收模式: true → false  (说明 send 还在关接收，Phase 1 失败)
```

**如果看到 `true → false`**：
- 说明某处代码仍在 send 时调 `EnableReceive(false)`
- grep `EnableReceive.*false` 找到残留代码

---

### 问题 3：config default_on 不生效

**症状**：
- config.json 写了 `"default_on": false`
- 但启动后接收仍开启

**排查**：
1. 检查 config 是否真的加载了
   ```powershell
   Get-Content $env:TEMP\quickdrop.log | Select-String "config:"
   ```

2. 检查 config.json 格式
   ```powershell
   Get-Content ~\.quickdrop\config.json
   ```

3. 确认不是 `quickdrop recv`（recv 会强制开启）

---

### 问题 4：test-peer FAIL

**常见原因**：
- 端口冲突（8443/8444 被占）
- 旧版 daemon 残留

**解决**：
```powershell
Get-Process quickdrop -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 2
netstat -ano | Select-String ":8443|:8444"
.\test\test-peer.ps1
```

---

## 9. 验收通过标准

Phase 1 重构验收通过需满足：

### 必须项（Blocker）
- [ ] 编译成功（build.ps1）
- [ ] test-peer 24/24 PASS
- [ ] test-config 24/24 PASS
- [ ] **send 时接收不被关闭**（3.3 测试通过）

### 应该项（Major）
- [ ] config default_on 生效（3.2）
- [ ] recv 命令强制开启（3.4）
- [ ] 托盘手动切换生效（5.2）

### 可选项（Nice-to-have）
- [ ] PC↔PC 双向传输正常（4.1）
- [ ] 快速切换 send/recv 无异常（5.1）

---

## 10. 测试报告模板

```markdown
# Phase 1 重构测试报告

**测试人**：[你的名字]
**测试日期**：2026-06-XX
**版本**：v0.13.0-dev (Phase 1)

## 1. 编译验证
- [ ] PASS / [ ] FAIL
- 备注：

## 2. test-peer
- [ ] 24/24 PASS / [ ] X/24 FAIL
- 失败项：

## 3. send 时接收不关闭（核心）
- [ ] PASS / [ ] FAIL
- 现象：
- 日志截图：

## 4. config default_on
- [ ] PASS / [ ] FAIL
- 备注：

## 5. recv 强制开启
- [ ] PASS / [ ] FAIL
- 备注：

## 6. 回归功能
- [ ] 右键菜单正常
- [ ] 托盘菜单正常
- [ ] 手机上传正常
- [ ] PC↔PC 正常

## 7. 问题记录
（如有任何异常，详细描述）

## 8. 结论
- [ ] 通过验收，可进入 Phase 2
- [ ] 需修复 Bug：[列表]
- [ ] 需回退重做
```

---

## 11. 下一步

**通过验收后**：
1. commit Phase 1（tag v0.13.0-phase1）
2. 进入 Phase 2：Server 构造解耦（~1 小时，已在 Phase 1 完成大部分）
3. 进入 Phase 3：统一 dashboard（~6 小时，Vue 大改）

**未通过验收**：
1. 记录失败项 + 日志
2. 我修复 Bug
3. 重新测试

---

**有疑问随时问我！**
