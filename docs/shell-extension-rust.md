# Shell Extension 实现规范（Rust）

> **目标**：替换当前右键菜单的 CLI shell exec 方式，改用原生 Windows Shell Extension（COM DLL）。
> **语言**：Rust（内存安全编译器保证 > C++ 人工纪律）
> **架构**：极简 DLL（< 300 行 Rust，只转发给 daemon）
> **时机**：v0.13.0（Phase 1-3 重构完成后）

---

## 1. 为什么要替换 CLI 方式

### 当前问题（v0.12.0）

注册表写法：
```
HKCR\*\shell\QuickDrop\command = "C:\...\quickdrop.exe" send "%1"
```

**缺陷**：
- 每次右键发文件 fork 新进程（~50ms 延迟 + 进程开销）
- IPC 语义藏在 shell 命令里（不优雅）
- 无法做动态菜单（比如"发给 Alice / Bob"子菜单）
- 用户感知是"调命令行工具"而非"原生集成"

### Shell Extension 优势

- **性能**：DLL 已被 Explorer 加载，调用 < 5ms
- **功能**：可动态生成子菜单（读设备表实时显示对端列表）
- **体验**：跟 Dropbox / OneDrive 同级别的原生集成
- **安全**：Rust 编译器保证内存安全（不会炸 Explorer）

---

## 2. 技术栈

| 组件 | 技术 | 版本 |
|---|---|---|
| **语言** | Rust | 1.80+ |
| **Windows 绑定** | `windows-rs` crate | 0.58+ |
| **HTTP 客户端** | `reqwest` (blocking) | 0.12+ |
| **构建工具** | Cargo | 自带 |
| **目标架构** | x64 + x86（两份 DLL） | - |

### 为什么选 Rust 而非 C++

| 维度 | C++ (ATL) | Rust (windows-rs) |
|---|---|---|
| **内存安全** | ❌ 靠人工（野指针/UAF 可能炸 Explorer） | ✅ 编译器保证 |
| **学习曲线** | ⚠️ 需学 COM API + ATL 模板库 | ⚠️ 需学 COM API + `windows-rs` |
| **调试体验** | ✅ VS Debugger 完美 | ⚠️ rust-gdb 能用但不如 VS |
| **生态** | ✅ 25 年沉淀，文档全 | ⚠️ `windows-rs` 仅 3 年，坑多 |
| **维护性** | ❌ 改一行怕炸一片 | ✅ 重构安全 |

**判断**：产品级应用，长期维护，选 Rust。

---

## 3. 项目结构

### 3.1 目录布局

```
QuickDrop/
├── cmd/quickdrop/          # Go daemon（现有）
├── internal/               # Go 内部包（现有）
├── web/                    # Vue 前端（现有）
├── shell_extension/        # 🆕 Rust Shell Extension（独立 crate）
│   ├── Cargo.toml         # Rust 包配置
│   ├── src/
│   │   ├── lib.rs         # Shell Extension 实现
│   │   ├── com.rs         # COM 接口实现
│   │   └── http.rs        # daemon HTTP 调用
│   ├── build.rs           # 构建脚本（生成 .def 文件）
│   └── README.md          # 开发/调试指南
├── build.ps1              # 主构建脚本（改动：加 Rust 编译）
├── test/
│   └── test-shell-ext.ps1 # 🆕 Shell Extension 集成测试
└── 输出：
    quickdrop.exe          # Go daemon（现有）
    quickdrop_menu_x64.dll # 🆕 Rust Shell Extension (x64)
    quickdrop_menu_x86.dll # 🆕 Rust Shell Extension (x86)
```

### 3.2 Git 集成

**`.gitignore` 新增**：
```
# Rust
shell_extension/target/
shell_extension/Cargo.lock
*.dll
*.pdb
```

---

## 4. 环境部署

### 4.1 安装 Rust（首次，15 分钟）

```powershell
# 1. 安装 rustup
Invoke-WebRequest -Uri https://win.rustup.rs/x86_64 -OutFile rustup-init.exe
.\rustup-init.exe
# 按提示操作，选默认安装

# 2. 添加 x86 target（Shell Extension 需要 32 位版）
rustup target add i686-pc-windows-msvc

# 3. 验证
cargo --version  # 应输出 cargo 1.xx
rustc --version  # 应输出 rustc 1.xx
```

**依赖检查**：
- ✅ Visual Studio 2022（已有，Rust 用 MSVC 链接器）
- ✅ Windows SDK（VS 2022 自带）

### 4.2 首次编译（拉依赖，5-10 分钟）

```powershell
cd shell_extension
cargo fetch  # 提前拉依赖，之后增量编译只需 30 秒
```

---

## 5. 实现规范

### 5.1 Cargo.toml

```toml
[package]
name = "quickdrop_menu"
version = "0.13.0"
edition = "2021"
authors = ["QuickDrop Authors"]

[lib]
crate-type = ["cdylib"]  # 编译成 DLL

[dependencies]
windows = { version = "0.58", features = [
    "Win32_Foundation",
    "Win32_UI_Shell",
    "Win32_System_Com",
    "Win32_System_LibraryLoader",
    "Win32_Networking_WinHttp"
]}

[profile.release]
opt-level = "z"     # 优化大小（DLL 越小越好）
lto = true          # Link-Time Optimization
codegen-units = 1   # 单线程编译（慢但生成最小二进制）
strip = true        # 去符号表
panic = "abort"     # panic 时直接 abort（不走 unwind，更安全）
```

**预期输出大小**：~800 KB（x64），~700 KB（x86）

---

### 5.2 lib.rs 骨架

```rust
// shell_extension/src/lib.rs
use windows::{
    core::*,
    Win32::UI::Shell::*,
    Win32::System::Com::*,
};

/// QuickDropMenu: 实现 IContextMenu + IShellExtInit 的 COM 对象
#[implement(IContextMenu, IShellExtInit)]
struct QuickDropMenu {
    file_path: std::sync::Mutex<String>,
}

impl IShellExtInit_Impl for QuickDropMenu {
    fn Initialize(
        &self,
        _pidlfolder: *const ITEMIDLIST,
        pdtobj: Option<&IDataObject>,
        _hkeyid: usize,
    ) -> Result<()> {
        // 从 IDataObject 提取文件路径
        if let Some(data) = pdtobj {
            let path = extract_file_path(data)?;
            *self.file_path.lock().unwrap() = path;
        }
        Ok(())
    }
}

impl IContextMenu_Impl for QuickDropMenu {
    fn QueryContextMenu(
        &self,
        hmenu: HMENU,
        indexmenu: u32,
        idcmdfirst: u32,
        _idcmdlast: u32,
        _uflags: u32,
    ) -> Result<()> {
        // 插入菜单项 "通过 QuickDrop 发送"
        unsafe {
            InsertMenuW(
                hmenu,
                indexmenu,
                MF_STRING | MF_BYPOSITION,
                idcmdfirst as usize,
                w!("通过 QuickDrop 发送"),
            );
        }
        Ok(())
    }

    fn InvokeCommand(&self, pici: *const CMINVOKECOMMANDINFO) -> Result<()> {
        // 用户点了菜单，调 daemon HTTP API
        let path = self.file_path.lock().unwrap().clone();
        if path.is_empty() {
            return Err(Error::from(E_FAIL));
        }

        // 异步调用（不阻塞右键菜单）
        std::thread::spawn(move || {
            let _ = post_to_daemon(&path);
        });

        Ok(())
    }

    fn GetCommandString(
        &self,
        _idcmd: usize,
        _uflags: u32,
        _reserved: *const u32,
        _pszname: PSTR,
        _cchmax: u32,
    ) -> Result<()> {
        Ok(())
    }
}

// HTTP 调用（异步，不阻塞 Explorer）
fn post_to_daemon(file_path: &str) -> Result<()> {
    use std::io::Write;

    // 1. 读取 daemon 端口（从注册表 HKCU\Software\QuickDrop\DaemonPort）
    let port = read_daemon_port().unwrap_or(8443);

    // 2. HTTP POST /internal/send
    let url = format!("http://127.0.0.1:{}/internal/send", port);
    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(3))
        .build()?;

    let resp = client.post(&url)
        .body(file_path.to_string())
        .send()?;

    if !resp.status().is_success() {
        // daemon 返回错误，可选：弹 toast 提示用户
        eprintln!("daemon returned {}: {}", resp.status(), resp.text()?);
    }

    Ok(())
}

fn read_daemon_port() -> Option<u16> {
    // TODO: 从注册表 HKCU\Software\QuickDrop\DaemonPort 读取
    // daemon 启动时应写入当前监听端口
    None
}

fn extract_file_path(data: &IDataObject) -> Result<String> {
    // TODO: 从 IDataObject 提取文件路径
    // 参考：https://learn.microsoft.com/windows/win32/api/shobjidl_core/nf-shobjidl_core-idataobject-getdata
    Ok(String::new())
}

// DLL 入口点（COM 注册）
#[no_mangle]
pub extern "system" fn DllGetClassObject(
    rclsid: *const GUID,
    riid: *const GUID,
    ppv: *mut *mut core::ffi::c_void,
) -> HRESULT {
    // TODO: 实现 COM class factory
    E_NOTIMPL
}

#[no_mangle]
pub extern "system" fn DllCanUnloadNow() -> HRESULT {
    S_FALSE  // 永远不卸载（简化实现）
}

#[no_mangle]
pub extern "system" fn DllRegisterServer() -> HRESULT {
    // TODO: 写注册表 HKCR\CLSID\{GUID} + HKCR\*\shellex\ContextMenuHandlers
    S_OK
}

#[no_mangle]
pub extern "system" fn DllUnregisterServer() -> HRESULT {
    // TODO: 删注册表
    S_OK
}
```

**关键点**：
- `#[implement]` 宏自动生成 COM 接口代码
- `InvokeCommand` 里用 `std::thread::spawn` 异步调 HTTP（不阻塞 Explorer）
- panic 时 `panic = "abort"` 保证不 unwind 进 COM（会炸 Explorer）

---

### 5.3 COM 注册（手动实现或用 `windows-registry` crate）

**CLSID**（全局唯一）：
```
{A3F5E8C2-1D4B-4F9A-8E3C-7B2D9A4C6E1F}
```
（用 `uuidgen` 或 https://guidgenerator.com/ 生成）

**注册表写入**：
```
HKCR\CLSID\{GUID}
  (Default) = "QuickDrop Shell Extension"
  InprocServer32
    (Default) = "C:\...\quickdrop_menu.dll"
    ThreadingModel = "Apartment"

HKCR\*\shellex\ContextMenuHandlers\QuickDrop
  (Default) = "{A3F5E8C2-1D4B-4F9A-8E3C-7B2D9A4C6E1F}"
```

**实现**：
- 方案 A：Rust 代码里用 `winreg` crate 写注册表
- 方案 B：PowerShell 脚本（`build.ps1` 里调用 `reg add`）

推荐方案 B（简单，调试方便）。

---

## 6. 构建集成

### 6.1 build.ps1 改动

在现有 Go build 之后加：

```powershell
# ===== Rust Shell Extension build =====
Write-Host ""
Write-Host "=== Rust build [Shell Extension] ===" -ForegroundColor Cyan

if (-not (Get-Command cargo -ErrorAction SilentlyContinue)) {
    Write-Host "ERROR: cargo 未安装. 请先安装 Rust: https://rustup.rs/" -ForegroundColor Red
    exit 1
}

Push-Location shell_extension

# x64 build
Write-Host "  Building x64..." -ForegroundColor DarkGray
cargo build --release --target x86_64-pc-windows-msvc
if ($LASTEXITCODE -ne 0) {
    Pop-Location
    throw "Rust x64 build failed"
}
Copy-Item target\x86_64-pc-windows-msvc\release\quickdrop_menu.dll ..\quickdrop_menu_x64.dll

# x86 build
Write-Host "  Building x86..." -ForegroundColor DarkGray
cargo build --release --target i686-pc-windows-msvc
if ($LASTEXITCODE -ne 0) {
    Pop-Location
    throw "Rust x86 build failed"
}
Copy-Item target\i686-pc-windows-msvc\release\quickdrop_menu.dll ..\quickdrop_menu_x86.dll

Pop-Location

$x64Size = (Get-Item quickdrop_menu_x64.dll).Length / 1MB
$x86Size = (Get-Item quickdrop_menu_x86.dll).Length / 1MB
Write-Host "Shell Extension x64: $('{0:N2} MB' -f $x64Size)" -ForegroundColor Green
Write-Host "Shell Extension x86: $('{0:N2} MB' -f $x86Size)" -ForegroundColor Green
```

### 6.2 注册脚本（test/register-shell-ext.ps1）

```powershell
# 需管理员权限
#Requires -RunAsAdministrator

param(
    [switch]$Unregister
)

$CLSID = "{A3F5E8C2-1D4B-4F9A-8E3C-7B2D9A4C6E1F}"
$DllPathX64 = Resolve-Path "..\quickdrop_menu_x64.dll"
$DllPathX86 = Resolve-Path "..\quickdrop_menu_x86.dll"

if ($Unregister) {
    Write-Host "Unregistering Shell Extension..." -ForegroundColor Yellow
    
    # 删除注册表
    reg delete "HKCR\CLSID\$CLSID" /f 2>$null
    reg delete "HKCR\*\shellex\ContextMenuHandlers\QuickDrop" /f 2>$null
    
    # 重启 Explorer
    Stop-Process -Name explorer -Force
    Start-Sleep -Seconds 2
    Start-Process explorer
    
    Write-Host "Done. 右键菜单项已移除." -ForegroundColor Green
} else {
    Write-Host "Registering Shell Extension..." -ForegroundColor Cyan
    
    # x64 注册
    reg add "HKCR\CLSID\$CLSID" /ve /d "QuickDrop Shell Extension" /f
    reg add "HKCR\CLSID\$CLSID\InprocServer32" /ve /d "$DllPathX64" /f
    reg add "HKCR\CLSID\$CLSID\InprocServer32" /v "ThreadingModel" /d "Apartment" /f
    
    # x86 注册（同样的 CLSID，但 InprocServer32 指向 x86 DLL）
    # Windows 会根据 Explorer 进程架构自动选择
    reg add "HKCR\Wow6432Node\CLSID\$CLSID" /ve /d "QuickDrop Shell Extension" /f
    reg add "HKCR\Wow6432Node\CLSID\$CLSID\InprocServer32" /ve /d "$DllPathX86" /f
    reg add "HKCR\Wow6432Node\CLSID\$CLSID\InprocServer32" /v "ThreadingModel" /d "Apartment" /f
    
    # 关联到右键菜单
    reg add "HKCR\*\shellex\ContextMenuHandlers\QuickDrop" /ve /d "$CLSID" /f
    
    # 重启 Explorer
    Stop-Process -Name explorer -Force
    Start-Sleep -Seconds 2
    Start-Process explorer
    
    Write-Host "Done. 右键任意文件应看到'通过 QuickDrop 发送'." -ForegroundColor Green
}
```

---

## 7. 开发工作流

### 7.1 独立开发 Shell Extension

```powershell
cd shell_extension

# 编译（debug build，带符号表，便于调试）
cargo build --target x86_64-pc-windows-msvc

# 测试（单元测试，不需要注册 COM）
cargo test

# 静态分析
cargo clippy

# 格式化
cargo fmt
```

### 7.2 联调（DLL + daemon）

```powershell
# Terminal 1: 编译 + 注册 Shell Extension
.\build.ps1
.\test\register-shell-ext.ps1  # 需管理员权限

# Terminal 2: 起 daemon
.\quickdrop.exe recv

# Terminal 3: 测试
# 右键任意文件 → 点"通过 QuickDrop 发送" → daemon 应收到 /internal/send → 弹 QR 窗

# 查看日志
Get-Content $env:TEMP\quickdrop.log -Tail 20
```

### 7.3 调试

**问题**：DLL 崩了会炸整个 Explorer（所有窗口全关）

**解决**：
1. **虚拟机测试**（推荐）
2. **Rust panic 自动记日志**：
   ```rust
   std::panic::set_hook(Box::new(|info| {
       let log = format!("PANIC: {:?}", info);
       std::fs::write("C:\\quickdrop_panic.log", log).ok();
   }));
   ```
3. **WinDbg 事后分析**：
   ```
   windbg -pn explorer.exe
   .sympath+ C:\path\to\quickdrop_menu_x64.pdb
   g  # 运行到崩溃
   !analyze -v
   ```

---

## 8. 测试

### 8.1 单元测试（Rust）

```rust
// shell_extension/src/lib.rs
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_extract_file_path() {
        // TODO: mock IDataObject, 验证路径提取逻辑
    }

    #[test]
    fn test_post_to_daemon() {
        // TODO: mock HTTP server, 验证请求格式
    }
}
```

### 8.2 集成测试（PowerShell）

```powershell
# test/test-shell-ext.ps1
Write-Host "=== Shell Extension 集成测试 ===" -ForegroundColor Cyan

# 1. 编译 + 注册
.\build.ps1
.\test\register-shell-ext.ps1

# 2. 起 daemon
$daemon = Start-Process .\quickdrop.exe -ArgumentList "recv" -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 3

# 3. 模拟右键发送（通过调用 COM 接口）
# TODO: C# 或 PowerShell 脚本调用 IContextMenu::InvokeCommand

# 4. 验证 daemon 收到请求
$log = Get-Content $env:TEMP\quickdrop.log -Tail 50
if ($log -match "POST /internal/send") {
    Write-Host "PASS: daemon 收到 Shell Extension 请求" -ForegroundColor Green
} else {
    Write-Host "FAIL: daemon 未收到请求" -ForegroundColor Red
}

# 5. 清理
Stop-Process -Id $daemon.Id -Force
.\test\register-shell-ext.ps1 -Unregister
```

### 8.3 压力测试

```powershell
# 连续右键 100 次，验证不崩溃、不内存泄漏
for ($i = 1; $i -le 100; $i++) {
    Write-Host "Round $i/100" -ForegroundColor DarkGray
    # TODO: 自动化右键操作（通过 UI Automation）
    Start-Sleep -Milliseconds 500
}

# 检查 Explorer 内存占用
$mem = (Get-Process explorer).WorkingSet64 / 1MB
if ($mem -gt 500) {
    Write-Host "WARNING: Explorer 内存占用 $mem MB (可能有泄漏)" -ForegroundColor Yellow
}
```

---

## 9. 发布

### 9.1 代码签名

**工具**：SignTool (Windows SDK)

```powershell
# 签名两份 DLL
signtool sign /f QuickDrop.pfx /p <password> /t http://timestamp.digicert.com quickdrop_menu_x64.dll
signtool sign /f QuickDrop.pfx /p <password> /t http://timestamp.digicert.com quickdrop_menu_x86.dll

# 验证
signtool verify /pa quickdrop_menu_x64.dll
```

**证书选择**：
- **EV 证书**（推荐）：$300/年，SmartScreen 信任快
- **普通证书**：$50/年，但需积累下载数才不报毒

### 9.2 安装器集成

修改 Inno Setup 脚本（或 WiX），在安装时：
1. 复制 `quickdrop_menu_x64.dll` 和 `quickdrop_menu_x86.dll` 到安装目录
2. 调用 `regsvr32` 或直接写注册表
3. 卸载时清理注册表 + 重启 Explorer

---

## 10. 验收标准

### v0.13.0 交付清单

- [ ] `shell_extension/` 目录存在，包含完整 Rust 项目
- [ ] `cargo build --release` 编译通过（x64 + x86）
- [ ] 输出 DLL < 1 MB（x64），< 900 KB（x86）
- [ ] 右键任意文件看到"通过 QuickDrop 发送"菜单项
- [ ] 点菜单 → daemon 收到 `/internal/send` 请求 → 弹 QR 窗
- [ ] 连续右键 100 次不崩 Explorer
- [ ] daemon 不在时，DLL 不崩 Explorer（返回错误即可）
- [ ] `cargo test` 全 PASS
- [ ] `cargo clippy` 无 warning
- [ ] `test/test-shell-ext.ps1` PASS
- [ ] 已签名（临时自签名证书也行，发布前换正式证书）

### 回归验证

- [ ] 原有 test-config 24/24 PASS
- [ ] 原有 test-peer 24/24 PASS
- [ ] Go daemon 功能不受影响

---

## 11. 风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|---|---|---|---|
| **DLL 崩溃炸 Explorer** | 中 | 高 | 1. 虚拟机测试<br>2. panic hook 记日志<br>3. 7×24h 压测 |
| **内存泄漏** | 低 | 中 | 1. Rust 自动管理<br>2. valgrind 扫描 |
| **daemon 端口动态** | 高 | 中 | DLL 从注册表读端口（daemon 写） |
| **签名被报毒** | 中 | 高 | 1. EV 证书<br>2. 提交 VirusTotal 白名单 |
| **`windows-rs` API 变更** | 低 | 中 | 锁版本号 `windows = "0.58"` |

---

## 12. 参考资料

### 官方文档

- [Windows Shell Extensions](https://learn.microsoft.com/windows/win32/shell/shell-exts)
- [IContextMenu Interface](https://learn.microsoft.com/windows/win32/api/shobjidl_core/nn-shobjidl_core-icontextmenu)
- [windows-rs GitHub](https://github.com/microsoft/windows-rs)

### 示例代码

- [windows-rs samples](https://github.com/microsoft/windows-rs/tree/master/crates/samples)
- [Rust Shell Extension (非官方)](https://github.com/sivadeilra/windows-samples-rs/tree/main/shell_extension)

### 工具

- [uuidgen](https://learn.microsoft.com/windows/win32/api/rpcdce/nf-rpcdce-uuidcreate) - 生成 CLSID
- [rust-analyzer](https://rust-analyzer.github.io/) - VS Code 插件
- [WinDbg](https://learn.microsoft.com/windows-hardware/drivers/debugger/) - 调试工具

---

## 13. 时间表（v0.13.0）

```
Week 1-2: Phase 1-3 架构重构（发送/接收解耦 + 统一 dashboard）
          ↓ API 稳定，/internal/send 签名确定

Week 3:   Shell Extension 开发
  Day 1-2: 搭骨架 + 实现 COM 接口
  Day 3-4: 集成 HTTP 调用 + 测试
  Day 5:   x86 编译 + 双架构注册
  Day 6-7: 压测 + bug 修复

Week 4:   集成测试 + 签名 + 发布
  Day 1-2: 回归测试（test-config + test-peer）
  Day 3:   代码签名 + 安装器集成
  Day 4-5: 内测（5 个真实用户）
  Day 6-7: 修内测反馈 + 正式发布 v0.13.0
```

---

**下一步**：Phase 1-3 重构（现在开始）→ Week 3 回来执行此文档。
