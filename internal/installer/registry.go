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

	"golang.org/x/sys/windows/registry"
)

const (
	// menuKeyPath 用户右键 -> "通过 QuickDrop 发送" 的注册位置.
	// 在 HKCU 下, 不需要管理员权限.
	menuKeyPath    = `Software\Classes\*\shell\QuickDrop`
	commandSubKey  = `command`
	menuLabel      = "通过 QuickDrop 发送"
)

// Install 写入注册表, 让 Windows 右键文件菜单出现 "通过 QuickDrop 发送".
// exePath 必须是绝对路径 (不会自动 abs, 调用者负责).
// 幂等: 已存在会覆盖, 不报错.
func Install(exePath string) error {
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
	// 双引号包路径处理空格 + %1 是 Windows shell 传给我们的文件路径
	cmd := fmt.Sprintf(`"%s" send "%%1"`, exePath)
	if err := cmdKey.SetStringValue("", cmd); err != nil {
		return fmt.Errorf("写 command 失败: %w", err)
	}

	return nil
}

// Uninstall 删除注册表中右键菜单条目. 幂等: 不存在不报错.
// 必须先删 command 子键再删主键.
func Uninstall() error {
	// 先删 command 子键
	if err := registry.DeleteKey(registry.CURRENT_USER, menuKeyPath+`\`+commandSubKey); err != nil &&
		err != registry.ErrNotExist {
		return fmt.Errorf("删 command 子键失败: %w", err)
	}
	// 再删主键
	if err := registry.DeleteKey(registry.CURRENT_USER, menuKeyPath); err != nil &&
		err != registry.ErrNotExist {
		return fmt.Errorf("删主键失败: %w", err)
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
