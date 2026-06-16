# QuickDrop 文档导航

> **原则**: 代码、注释、文档讲同一件事。文档滞后 = 负债。

## 用户文档 (待补)

QuickDrop 当前没有用户手册。计划在 v1.0 发布时写一份简短的"5 分钟上手"。

## 开发者文档

### 核心文档 (按阅读顺序)

#### 1. [QuickDrop.md](./QuickDrop.md) — 产品蓝图 ⭐
- 核心体验目标 + 不做的事
- 使用模式 (PC↔手机 / PC↔PC)
- 运行形态 (托盘 + 配置中心结构)
- 技术栈 (已定档, 不再争论)
- Phase 路线 (1-4)
- ADR 决策表 (35 条)

**更新频率**: 重大功能/架构决策时.

#### 2. [PLAN.md](./PLAN.md) — 执行进度 📝
- 开发环境
- 项目结构 (文件树 + 职责)
- 已完成任务 (按版本 v0.1 → 当前)
- 配置项扩展池
- 待补验收
- 已知小项

**更新频率**: 每个版本 / 每个任务完成后.

#### 3. [TEST.md](./TEST.md) — 测试用例 ✅
- 自动化测试脚本汇总
- 手工测试用例 (11 项)
- 通过标准

**更新频率**: 加新功能时同步加测试用例.

### 专项文档

#### [docs/shell-extension-rust.md](./docs/shell-extension-rust.md) — Rust Shell Extension 规划
- v0.13.0 / Phase 4 候选
- 替换 CLI 右键 (50ms fork) → 原生 IContextMenu (<5ms)
- 完整代码骨架 + 验收标准 + 时间表

**当前状态**: 待启动. CLI 模式够用, 等用户增长再投入.

## 文档维护规约

### 更新触发

| 事件 | 更新文档 |
|---|---|
| 加新功能 | QuickDrop.md (用户感知) + PLAN.md (实现) + TEST.md (用例) |
| 改架构 | QuickDrop.md (ADR) + PLAN.md + 代码注释 |
| 发现 Bug | PLAN.md (问题记录) |
| 踩坑 | PLAN.md (避坑指南) |

### 一致性检查

定期 (每次重大改动后) 运行:

```powershell
# 1. 扫描已删除字段的引用
Select-String -Path internal\*.go,cmd\quickdrop\*.go -Pattern "absPath|fileName|receiveMode" -Recurse

# 2. 扫描过时函数签名
Select-String -Path *.md,docs\*.md -Pattern "Server\.New.*rawPath|runDaemon.*initialReceive"

# 3. 扫描 TODO/FIXME
Get-ChildItem -Recurse -Include *.go | Select-String "TODO|FIXME"
```

发现过时描述 → 立刻清理.

### 代码注释规约

**好注释**:
```go
// transfer 管理发送队列 (当前单文件, 未来多文件).
transfer *transfer.Manager
```

**坏注释**:
```go
// 替代原 absPath/fileName/fileSize 字段.  // ❌ 提到已删除代码
transfer *transfer.Manager
```

```go
// TODO: 重构这里  // ❌ 模糊, 3 个月后忘了要重构什么
```

写注释场景:
- public API 必须
- 复杂算法必须
- 副作用 (触发回调/写盘/HTTP) 必须
- 临时 workaround 必须 + 标 TODO(版本)
- 显而易见逻辑不写

## 文档分工

| 文档 | 主维护者 | 协作者 |
|---|---|---|
| QuickDrop.md | 产品负责人 | 架构师 |
| PLAN.md | 开发者 | 所有人 |
| TEST.md | QA | 开发者 |
| 技术规范 (docs/) | 模块负责人 | - |

---

**文档即代码** — 过时的文档比没有文档更危险.
