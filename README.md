# QuickDrop 文档导航

> **原则**：代码、注释、文档讲同一件事。文档滞后 = 负债。

---

## 核心文档（按阅读顺序）

### 1. [QuickDrop.md](./QuickDrop.md) — 产品蓝图 ⭐

**内容**：
- 核心体验目标（量化"无感"）
- 明确不做的事（边界）
- 技术选型（为什么这么选）
- Phase 路线图（1-4）
- ADR 架构决策表（20 条）

**更新频率**：重大功能/架构决策时更新  
**读者**：所有人

---

### 2. [PLAN.md](./PLAN.md) — 执行进度 📝

**内容**：
- 开发环境（Go/Node/WebView2 版本）
- 项目结构（文件树 + 职责）
- 已完成任务（v0.1-v0.12.0）
- 当前问题记录（踩坑 + 解决方案）
- 待办事项（v0.12+ UX 方向）
- **Phase 1-3 重构计划**（架构升级路线）
- **v0.13.0 Shell Extension 规划**（Rust 替换 CLI 右键）

**更新频率**：每个版本/每个任务完成后更新  
**读者**：开发者、LLM 协作

---

### 3. [TEST.md](./TEST.md) — 测试用例 ✅

**内容**：
- 6 个端到端测试场景（单文件/大文件/并发/网络异常/设备信任/config 热更新）
- test-config.ps1 / test-peer.ps1 自动化测试说明
- 手工测试检查清单

**更新频率**：加新功能时同步加测试用例  
**读者**：QA、验收测试

---

## 专项文档

### 4. [docs/shell-extension-rust.md](./docs/shell-extension-rust.md) — Shell Extension 技术规范 🦀

**内容**：
- 为什么替换 CLI 方式（性能 + 功能 + 体验）
- Rust vs C++ 技术选型对比（8 个维度）
- 完整实现规范（Cargo.toml + lib.rs 骨架）
- 环境部署指南（rustup 15 分钟）
- 构建集成（build.ps1 改动）
- COM 注册脚本（x64/x86 双架构）
- 开发工作流（编译/测试/调试）
- 验收标准（12 项 checklist）
- 风险缓解（DLL 崩溃/内存泄漏/签名）
- 时间表（Week 3-4 详细拆解）

**时机**：v0.13.0（Phase 1-3 重构完成后）  
**读者**：实现 Shell Extension 的开发者

---

### 5. [docs/test-phase1-refactor.md](./docs/test-phase1-refactor.md) — Phase 1 重构验收测试 🧪

**内容**：
- 编译验证（build.ps1 预期输出）
- 自动化回归（test-peer + test-config）
- **核心验证：send 时接收不被关闭**（需手机）
- PC↔PC 双向传输（需第二台 PC）
- 边界情况测试（快速切换/托盘操作）
- 回归检查清单（8 项功能不破坏）
- 日志分析要点（关键日志行 + 异常模式）
- 常见问题排查（4 个典型场景）
- 验收通过标准（必须项/应该项/可选项）
- 测试报告模板

**时机**：Phase 1 重构代码完成后  
**读者**：验收测试人员

---

## 文档维护规约

### 何时更新文档

| 触发事件 | 更新文档 | 示例 |
|---|---|---|
| 加新功能 | QuickDrop.md（如果影响用户体验）<br>PLAN.md（记录实现细节）<br>TEST.md（加测试用例） | v0.12.0 加"选文件发送" → 更新 PLAN.md 待办 |
| 改架构 | QuickDrop.md（ADR 表）<br>PLAN.md（重构计划）<br>代码注释 | Phase 1 发送/接收解耦 → 更新 ADR-17 描述 |
| 发现 Bug | PLAN.md（问题记录） | daemon 异常死 → 记录原因 + 解决方案 |
| 踩坑 | PLAN.md（问题记录） | PowerShell UTF-8 BOM 问题 → 记录避坑指南 |
| 完成任务 | PLAN.md（勾选 ✅） | v0.11.2 完成 → 勾选对应待办 |

### 文档一致性检查清单

定期（每次重大改动后）运行：

```powershell
# 1. 扫描过时字段引用
Select-String -Path internal\*.go,cmd\quickdrop\*.go -Pattern "absPath|fileName|receiveMode" -Recurse

# 2. 扫描过时函数签名
Select-String -Path *.md,docs\*.md -Pattern "Server\.New.*rawPath|runDaemon.*initialReceive"

# 3. 检查未解决的 TODO
Get-ChildItem -Recurse -Include *.go | Select-String "TODO|FIXME"
```

**发现过时描述 → 立刻清理**（不要让它们存活超过 1 周）。

---

## 代码注释规约

### 好注释 ✅

```go
// transfer 管理发送队列 (当前单文件, 未来多文件).
transfer *transfer.Manager
```
**为什么好**：说明当前状态 + 未来扩展方向

```go
// EnableReceive 开/关接收. 触发 onReceive 回调让 main 起/关 receive webview.
func (s *Server) EnableReceive(on bool) {
```
**为什么好**：说明副作用（回调触发）

### 坏注释 ❌

```go
// 替代原 absPath/fileName/fileSize 字段.  // ❌ 实现细节，代码改了注释没改
transfer *transfer.Manager
```
**为什么坏**：提到已删除的代码，阅读者困惑"absPath 在哪"

```go
// TODO: 重构这里  // ❌ 模糊，不知道重构什么
```
**为什么坏**：没有上下文，3 个月后忘了要重构什么

### 何时写注释

| 场景 | 写不写 | 示例 |
|---|---|---|
| public API | ✅ 必须 | `func New(port int, cfg *receive.Config)` |
| 复杂算法 | ✅ 必须 | peer-send token 生成逻辑 |
| 副作用 | ✅ 必须 | "触发回调" / "写文件" / "发 HTTP" |
| 临时 workaround | ✅ 必须 + TODO | `// TODO(v0.14): 改成异步，现在同步阻塞` |
| 显而易见逻辑 | ❌ 不写 | `i++` 不需要注释"递增 i" |

---

## 文档分工

| 文档 | 主维护者 | 协作者 |
|---|---|---|
| QuickDrop.md | 产品负责人 | 架构师 |
| PLAN.md | 开发者 | 所有人 |
| TEST.md | QA | 开发者 |
| 技术规范（docs/） | 模块负责人 | - |

---

## 下一步

**Phase 1 重构完成后**：
1. 更新 PLAN.md：勾选 Phase 1 完成 ✅
2. commit：`doc: Phase 1 重构完成 + 清理过时注释`
3. 进入 Phase 2（或 Shell Extension v0.13.0）

**有新功能时**：
1. 先更新 QuickDrop.md（如果影响用户）
2. 实现代码 + 写注释
3. 更新 PLAN.md 记录进度
4. 加 TEST.md 测试用例
5. commit 时说明文档同步更新了

---

**文档即代码 — 过时的文档比没有文档更危险。**
