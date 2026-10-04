package logbus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRotationBySize 验证文件写满 maxBytes 后自动轮转，且当前文件不再超限。
func TestRotationBySize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	b := NewWithRotation(100, path, 64, 2)
	defer b.Close()

	for i := 0; i < 300; i++ {
		b.Logf("info", "line %03d with some padding to make it longer", i)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("当前日志文件应存在: %v", err)
	}
	if fi.Size() > 64 {
		t.Fatalf("当前文件 %d 字节，超过轮转上限 64", fi.Size())
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("应存在轮转文件 .1: %v", err)
	}

	// .1 承载最近一次轮转的那段内容（每轮 2 行左右，.1 里应见末尾行）。
	old, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("读取 .1 失败: %v", err)
	}
	if len(old) == 0 || !strings.Contains(string(old), "[info] line 29") {
		t.Fatalf(".1 应包含最近轮转出的日志（含 line 29x），实际: %q", string(old[:min(60, len(old))]))
	}
	// 轮转链应推进：.2 存在（更旧的批次），.3 不存在（keep=2）。
	if _, err := os.Stat(path + ".2"); err != nil {
		t.Fatalf("多轮轮转后应存在 .2: %v", err)
	}
	if _, err := os.Stat(path + ".3"); err == nil {
		t.Fatalf("keep=2 时不应存在 .3")
	}
}

// TestRotationKeepCount 验证历史份数上限：keep=2 时只保留 .1 与 .2。
func TestRotationKeepCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	b := NewWithRotation(100, path, 32, 2)
	defer b.Close()

	for i := 0; i < 500; i++ {
		b.Logf("info", "filler line %04d padding padding", i)
	}

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("应存在 .1: %v", err)
	}
	if _, err := os.Stat(path + ".2"); err != nil {
		t.Fatalf("应存在 .2: %v", err)
	}
	if _, err := os.Stat(path + ".3"); err == nil {
		t.Fatalf("keep=2 时不应存在 .3")
	}
}

// TestNoRotationWithoutConfig 验证不带轮转配置的 New 保持旧行为（无限追加）。
func TestNoRotationWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	b := New(100, path)
	defer b.Close()

	for i := 0; i < 200; i++ {
		b.Logf("info", "line without rotation config %03d", i)
	}
	if _, err := os.Stat(path + ".1"); err == nil {
		t.Fatalf("未配置轮转时不应产生 .1")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
