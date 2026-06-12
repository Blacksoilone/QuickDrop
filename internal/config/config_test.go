package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withTempHome 把 HOME / USERPROFILE 临时指到 t.TempDir, 跑完恢复.
// 这样 configDir 进的是隔离目录, 不污染用户真实 ~/.quickdrop.
func withTempHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	// Windows 用 USERPROFILE; Linux/Mac 用 HOME. 设两个以保万一.
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("HOME", tmp)
	return tmp
}

func TestLoad_DefaultWhenFileMissing(t *testing.T) {
	withTempHome(t)
	t.Setenv("QUICKDROP_PORT", "")
	t.Setenv("QUICKDROP_DOWNLOAD_DIR", "")

	m, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c := m.Get()
	want := Default()
	if c.Server.Port != want.Server.Port {
		t.Errorf("Port = %d, want %d", c.Server.Port, want.Server.Port)
	}
	if c.Download.Conflict != want.Download.Conflict {
		t.Errorf("Conflict = %q, want %q", c.Download.Conflict, want.Download.Conflict)
	}
	if !c.UI.ToastsEnabled {
		t.Error("ToastsEnabled default should be true")
	}
	if !c.Server.MdnsEnabled {
		t.Error("MdnsEnabled default should be true")
	}
}

func TestLoad_FileWins(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("QUICKDROP_PORT", "")
	t.Setenv("QUICKDROP_DOWNLOAD_DIR", "")

	cfgDir := filepath.Join(home, ".quickdrop")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	disk := Config{
		Download: DownloadConfig{Dir: filepath.Join(home, "Custom"), Conflict: ConflictOverwrite},
		Server:   ServerConfig{Port: 9000, MdnsEnabled: false},
		Receive:  ReceiveConfig{MaxFileSize: 1024},
		UI:       UIConfig{ToastsEnabled: false, RevealOnDone: false},
		System:   SystemConfig{Autostart: true},
	}
	data, _ := json.MarshalIndent(disk, "", "  ")
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c := m.Get()
	if c.Server.Port != 9000 {
		t.Errorf("Port = %d, want 9000 from disk", c.Server.Port)
	}
	if c.Download.Conflict != ConflictOverwrite {
		t.Errorf("Conflict = %q, want overwrite from disk", c.Download.Conflict)
	}
	if c.UI.ToastsEnabled {
		t.Error("ToastsEnabled should be false from disk")
	}
	if !c.System.Autostart {
		t.Error("Autostart should be true from disk")
	}
}

func TestLoad_EnvOverridesFile(t *testing.T) {
	home := withTempHome(t)

	cfgDir := filepath.Join(home, ".quickdrop")
	os.MkdirAll(cfgDir, 0o755)
	disk := Default()
	disk.Server.Port = 9000
	data, _ := json.MarshalIndent(disk, "", "  ")
	os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0o644)

	// env 应该赢
	t.Setenv("QUICKDROP_PORT", "9500")
	t.Setenv("QUICKDROP_DOWNLOAD_DIR", filepath.Join(home, "EnvDir"))

	m, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c := m.Get()
	if c.Server.Port != 9500 {
		t.Errorf("Port = %d, want 9500 from env", c.Server.Port)
	}
	if c.Download.Dir != filepath.Join(home, "EnvDir") {
		t.Errorf("Dir = %q, want env override", c.Download.Dir)
	}
}

func TestLoad_InvalidPortFallsBack(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("QUICKDROP_PORT", "")

	cfgDir := filepath.Join(home, ".quickdrop")
	os.MkdirAll(cfgDir, 0o755)
	// 故意写非法 port
	bad := Default()
	bad.Server.Port = 99999
	data, _ := json.MarshalIndent(bad, "", "  ")
	os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0o644)

	m, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Get().Server.Port != 8443 {
		t.Errorf("Port should fallback to 8443, got %d", m.Get().Server.Port)
	}
}

func TestSave_RejectInvalid(t *testing.T) {
	withTempHome(t)
	m, _ := Load()

	bad := m.Get()
	bad.Server.Port = -1
	if err := m.Save(bad); err == nil {
		t.Error("Save should reject port=-1")
	}

	bad2 := m.Get()
	bad2.Download.Conflict = "nuke"
	if err := m.Save(bad2); err == nil {
		t.Error("Save should reject conflict=nuke")
	}

	bad3 := m.Get()
	bad3.Receive.MaxFileSize = -1
	if err := m.Save(bad3); err == nil {
		t.Error("Save should reject max_file_size=-1")
	}
}

func TestSave_PersistsAndReloads(t *testing.T) {
	withTempHome(t)
	t.Setenv("QUICKDROP_PORT", "")

	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	updated := m.Get()
	updated.Server.Port = 9100
	updated.UI.ToastsEnabled = false
	updated.Receive.MaxFileSize = 100 * 1024 * 1024
	if err := m.Save(updated); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 再 Load 一次, 应该是改后的值
	m2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	c2 := m2.Get()
	if c2.Server.Port != 9100 {
		t.Errorf("after reload Port = %d, want 9100", c2.Server.Port)
	}
	if c2.UI.ToastsEnabled {
		t.Error("after reload ToastsEnabled should be false")
	}
	if c2.Receive.MaxFileSize != 100*1024*1024 {
		t.Errorf("after reload MaxFileSize = %d", c2.Receive.MaxFileSize)
	}
}

func TestSave_AtomicNoHalfFile(t *testing.T) {
	withTempHome(t)
	m, _ := Load()

	c := m.Get()
	c.Server.Port = 8500
	if err := m.Save(c); err != nil {
		t.Fatal(err)
	}

	// config.json.tmp 不该残留
	if _, err := os.Stat(m.Path() + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp 残留: err=%v", err)
	}
	// config.json 必须存在且能解析回完整 struct
	data, err := os.ReadFile(m.Path())
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip Config
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("写出来的 JSON 解析不回去: %v\n%s", err, data)
	}
	if roundtrip.Server.Port != 8500 {
		t.Errorf("on-disk Port = %d", roundtrip.Server.Port)
	}
}

func TestResolvedDownloadDir_Default(t *testing.T) {
	home := withTempHome(t)
	t.Setenv("QUICKDROP_DOWNLOAD_DIR", "")

	m, _ := Load()
	dir, err := m.ResolvedDownloadDir()
	if err != nil {
		t.Fatalf("ResolvedDownloadDir: %v", err)
	}
	want := filepath.Join(home, "Downloads", "QuickDrop")
	if dir != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
	// 应该真的创建了
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("目录没创建: %v", err)
	}
}

func TestResolvedDownloadDir_Custom(t *testing.T) {
	home := withTempHome(t)
	custom := filepath.Join(home, "MyDrops")
	t.Setenv("QUICKDROP_DOWNLOAD_DIR", custom)

	m, _ := Load()
	dir, err := m.ResolvedDownloadDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != custom {
		t.Errorf("dir = %q, want %q", dir, custom)
	}
}

func TestConflictPolicy_IsValid(t *testing.T) {
	cases := map[ConflictPolicy]bool{
		ConflictRename:    true,
		ConflictOverwrite: true,
		ConflictReject:    true,
		"":                false,
		"nuke":            false,
		"RENAME":          false, // case sensitive
	}
	for p, want := range cases {
		if got := p.IsValid(); got != want {
			t.Errorf("%q IsValid = %v, want %v", p, got, want)
		}
	}
}
