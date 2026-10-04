package config

import (
	"bytes"
	"os"
	"testing"
	"time"
)

// TestMain 隔离测试环境：宿主 shell（飞牛应用环境）会带 TRIM_PKGETC /
// TRIM_PKGVAR，Load 会因此把配置写到宿主应用目录而不是测试临时目录。
func TestMain(m *testing.M) {
	os.Unsetenv("TRIM_PKGETC")
	os.Unsetenv("TRIM_PKGVAR")
	os.Exit(m.Run())
}

// readConfigFile 读取测试目录下 Load 落盘的 config.yaml。
func readConfigFile(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := os.ReadFile(dir + "/config.yaml")
	if err != nil {
		t.Fatalf("读取 config.yaml 失败: %v", err)
	}
	return data
}

// waitForFile 轮询等待文件内容出现 expected，超时失败。
func waitForFile(t *testing.T, dir, expected string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if bytes.Contains(readConfigFile(t, dir), []byte(expected)) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s 内 config.yaml 未出现 %q", within, expected)
		}
		time.Sleep(40 * time.Millisecond)
	}
}

// TestUpdateDebouncedSave 验证防抖异步落盘：
// Update 内存立即生效、300ms 后合并写盘、Flush 同步写盘、Close 收尾落盘。
func TestUpdateDebouncedSave(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	cfg.StartSaver()
	defer cfg.Close()

	// 1) 内存立即生效，磁盘稍后跟上。
	if err := cfg.Update(func(c *Config) error {
		c.Mode = ModeProxy
		return nil
	}); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	if cfg.Snapshot().Mode != ModeProxy {
		t.Fatal("Update 后内存未立即生效")
	}
	waitForFile(t, dir, "mode: proxy", 3*time.Second)

	// 2) 防抖窗口内的多次修改合并，最终状态正确。
	if err := cfg.Update(func(c *Config) error {
		c.Mode = ModeHosts
		return nil
	}); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := cfg.Update(func(c *Config) error {
		c.Mode = ModeDirect
		return nil
	}); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	waitForFile(t, dir, "mode: direct", 3*time.Second)
	if cfg.Snapshot().Mode != ModeDirect {
		t.Fatal("合并写盘后内存状态错误")
	}

	// 3) Flush 同步落盘：立刻读文件应已更新。
	if err := cfg.Update(func(c *Config) error {
		c.Mode = ModeAuto
		return nil
	}); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	if err := cfg.Flush(); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}
	if !bytes.Contains(readConfigFile(t, dir), []byte("mode: auto")) {
		t.Fatal("Flush 后文件未同步更新")
	}
}

// TestUpdateSyncWithoutSaver 验证 saver 未启动时 Update 退回同步落盘（旧语义）。
func TestUpdateSyncWithoutSaver(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	// 不调 StartSaver。
	if err := cfg.Update(func(c *Config) error {
		c.Mode = ModeProxy
		return nil
	}); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	if !bytes.Contains(readConfigFile(t, dir), []byte("mode: proxy")) {
		t.Fatal("saver 未启动时 Update 未同步落盘")
	}
}

// TestSnapshotDetachesSaver 验证快照不会携带 saver 状态：
// 在快照上 Update 退回同步写盘，且不会唤醒原对象的 saver。
func TestSnapshotDetachesSaver(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	cfg.StartSaver()
	defer cfg.Close()

	snap := cfg.Snapshot()
	if err := snap.Update(func(c *Config) error {
		c.Server.SessionTTLHours = 48
		return nil
	}); err != nil {
		t.Fatalf("快照 Update 失败: %v", err)
	}
	// 快照走同步路径，文件立即可见。
	if !bytes.Contains(readConfigFile(t, dir), []byte("session_ttl_hours: 48")) {
		t.Fatal("快照 Update 未同步落盘")
	}
}
