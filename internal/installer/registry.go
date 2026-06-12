// Package installer 管理 Windows 用户级右键菜单注册.
//
// 注册位置 (HKCU, 无需 UAC):
//   HKCU\Software\Classes\*\shell\QuickDrop
//     (默认)                = "通过 QuickDrop 发送"
//     Icon                  = "<exe 路径>,0"
//   HKCU\Software\Classes\*\shell\QuickDrop\command
//     (默认)                = "\"<exe 路径>\" send \"%1\""
//
// "*" 指任意文件 (不含文件夹). 文件夹发送 = 压缩 + 发送, 后续 Phase 做.
//
// Win11 现代右键菜单 (顶级显示) 需要 MSIX sparse package + IExplorerCommand,
// 工程量大. 传统注册表方式在 Win11 下需 Shift+右键 / "显示更多选项" 才能看到,
// 但功能完整. MSIX 升级走 Phase 4.
package installer

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	// menuKeyPath 用户右键 -> "通过 QuickDrop 发送" 的注册位置.
	// 在 HKCU 下, 不需要管理员权限.
	menuKeyPath    = `Software\Classes\*\shell\QuickDrop`
	commandSubKey  = `command`
	menuLabel      = "通过 QuickDrop 发送"

	// schemeKeyPath quickdrop:// URL scheme handler 注册位置 (ADR-19).
	// 用于 toast 通知按钮 quickdrop://accept?token=xxx / reject?token=xxx,
	// 点击后 Windows 启动我们的 exe 带上 URL 参数.
	schemeKeyPath = `Software\Classes\quickdrop`
)

// Install 写入注册表, 让 Windows 右键文件菜单出现 "通过 QuickDrop 发送" + 注册 quickdrop:// URL scheme.
// exePath 必须是绝对路径 (不会自动 abs, 调用者负责).
// 幂等: 已存在会覆盖, 不报错.
func Install(exePath string) error {
	if err := installContextMenu(exePath); err != nil {
		return err
	}
	if err := installURLScheme(exePath); err != nil {
		return err
	}
	return nil
}

func installContextMenu(exePath string) error {
	// 1. 主键: HKCU\Software\Classes\*\shell\QuickDrop
	key, _, err := registry.CreateKey(registry.CURRENT_USER, menuKeyPath, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("创建主键失败: %w", err)
	}
	defer key.Close()

	// 菜单文字 (默认值)
	if err := key.SetStringValue("", menuLabel); err != nil {
		return fmt.Errorf("写菜单文字失败: %w", err)
	}
	// 图标 = exe 的第一个图标 (索引 0). exe 本身没图标也无所谓, Explorer 给默认图标.
	if err := key.SetStringValue("Icon", fmt.Sprintf(`"%s",0`, exePath)); err != nil {
		return fmt.Errorf("写图标失败: %w", err)
	}

	// 2. command 子键: HKCU\Software\Classes\*\shell\QuickDrop\command
	cmdKey, _, err := registry.CreateKey(registry.CURRENT_USER, menuKeyPath+`\`+commandSubKey, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("创建 command 子键失败: %w", err)
	}
	defer cmdKey.Close()

	// command 值格式: "C:\path\to\quickdrop.exe" send "%1"
	cmd := fmt.Sprintf(`"%s" send "%%1"`, exePath)
	if err := cmdKey.SetStringValue("", cmd); err != nil {
		return fmt.Errorf("写 command 失败: %w", err)
	}
	return nil
}

// installURLScheme 注册 quickdrop:// URL scheme.
//
//   HKCU\Software\Classes\quickdrop
//     (默认)        = "URL:QuickDrop Protocol"
//     URL Protocol  = "" (空字符串, 标记这是 URL scheme)
//     \shell\open\command\(默认) = "<exe>" url-action "%1"
//
// 用户在浏览器/toast 按钮点 quickdrop://accept?token=xxx → Windows 启动:
//   "<exe>" url-action "quickdrop://accept?token=xxx"
func installURLScheme(exePath string) error {
	root, _, err := registry.CreateKey(registry.CURRENT_USER, schemeKeyPath, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("创建 scheme 主键失败: %w", err)
	}
	defer root.Close()
	if err := root.SetStringValue("", "URL:QuickDrop Protocol"); err != nil {
		return fmt.Errorf("写 scheme 描述失败: %w", err)
	}
	// "URL Protocol" 必须存在 (即使是空字符串), 这是 Windows 识别 URL scheme 的标志
	if err := root.SetStringValue("URL Protocol", ""); err != nil {
		return fmt.Errorf("写 URL Protocol 标志失败: %w", err)
	}

	cmdKey, _, err := registry.CreateKey(registry.CURRENT_USER, schemeKeyPath+`\shell\open\command`, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("创建 scheme command 子键失败: %w", err)
	}
	defer cmdKey.Close()
	cmd := fmt.Sprintf(`"%s" url-action "%%1"`, exePath)
	if err := cmdKey.SetStringValue("", cmd); err != nil {
		return fmt.Errorf("写 scheme command 失败: %w", err)
	}
	return nil
}

// Uninstall 删除注册表中右键菜单 + URL scheme. 幂等: 不存在不报错.
func Uninstall() error {
	// 先删 command 子键再删主键 (registry.DeleteKey 不递归)
	if err := registry.DeleteKey(registry.CURRENT_USER, menuKeyPath+`\`+commandSubKey); err != nil &&
		err != registry.ErrNotExist {
		return fmt.Errorf("删 command 子键失败: %w", err)
	}
	if err := registry.DeleteKey(registry.CURRENT_USER, menuKeyPath); err != nil &&
		err != registry.ErrNotExist {
		return fmt.Errorf("删主键失败: %w", err)
	}

	// URL scheme 子键链: scheme\shell\open\command → scheme\shell\open → scheme\shell → scheme
	for _, sub := range []string{
		schemeKeyPath + `\shell\open\command`,
		schemeKeyPath + `\shell\open`,
		schemeKeyPath + `\shell`,
		schemeKeyPath,
	} {
		if err := registry.DeleteKey(registry.CURRENT_USER, sub); err != nil &&
			err != registry.ErrNotExist {
			return fmt.Errorf("删 %s 失败: %w", sub, err)
		}
	}
	return nil
}

// IsInstalled 返回当前是否已注册右键菜单.
// 检查 command 子键的默认值存在性.
func IsInstalled() (bool, string) {
	key, err := registry.OpenKey(registry.CURRENT_USER, menuKeyPath+`\`+commandSubKey, registry.QUERY_VALUE)
	if err != nil {
		return false, ""
	}
	defer key.Close()
	val, _, err := key.GetStringValue("")
	if err != nil {
		return false, ""
	}
	return true, val
}

// IsURLSchemeInstalled 返回 quickdrop:// URL scheme 是否已注册.
func IsURLSchemeInstalled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, schemeKeyPath+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	_, _, err = key.GetStringValue("")
	return err == nil
}

// autostartRunKey HKCU\Software\Microsoft\Windows\CurrentVersion\Run\QuickDrop.
// Windows 用户登录时按这里的列表挨个起进程, 值是命令行字符串.
// HKCU 不需要 UAC, 跟 IsInstalled 路径同源 (用户级).
const (
	autostartRunPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartRunName = "QuickDrop"
)

// SyncAutostart 把 config.System.Autostart 同步到注册表 HKCU\...\Run.
// enable=true: 写 "<exe>" recv  (无图形界面参数, 进入纯接收 daemon)
// enable=false: 删 QuickDrop 这一项 (不存在不报错)
// exe 路径用 os.Executable() 当前进程, 跟用户实际启动的二进制保持一致.
func SyncAutostart(enable bool) error {
	if enable {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("拿 exe 路径: %w", err)
		}
		key, _, err := registry.CreateKey(registry.CURRENT_USER, autostartRunPath, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("打开 Run 键: %w", err)
		}
		defer key.Close()
		// 注意 recv 表示启动进入接收守护模式; 不加 "recv" 的话双击会被当作 send 模式
		// 然后 usage 报错退出.
		val := fmt.Sprintf(`"%s" recv`, exe)
		if err := key.SetStringValue(autostartRunName, val); err != nil {
			return fmt.Errorf("写 Run 值: %w", err)
		}
		return nil
	}
	// disable: 删值. 键本身不删 (其他应用可能也用 Run 键).
	key, err := registry.OpenKey(registry.CURRENT_USER, autostartRunPath, registry.SET_VALUE)
	if err != nil {
		// 键不存在视为已禁用, 不报错
		return nil
	}
	defer key.Close()
	if err := key.DeleteValue(autostartRunName); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("删 Run 值: %w", err)
	}
	return nil
}
