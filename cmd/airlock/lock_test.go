package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLockDir(t *testing.T) {
	dir := t.TempDir()
	first, err := lockDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 已有进程持有锁。
	if f, err := lockDir(dir); !errors.Is(err, errHeld) {
		if f != nil {
			f.Close()
		}
		t.Fatalf("第二次加锁 = %v，want errHeld", err)
	}
	// 锁已释放。进程退出时系统关闭句柄，效果相同。
	first.Close()
	again, err := lockDir(dir)
	if err != nil {
		t.Fatalf("释放后加锁 = %v", err)
	}
	again.Close()
}

// serveDir 让 serve 使用临时的数据目录，返回这个目录。
// PATH 为空，所以加锁的检查失效时，serve 也找不到 docker，不会删除本机真实的沙箱。
func serveDir(t *testing.T) string {
	base := t.TempDir()
	t.Setenv("LocalAppData", base)   // Windows 上 os.UserCacheDir 的来源
	t.Setenv("XDG_CACHE_HOME", base) // Linux 上 os.UserCacheDir 的来源
	t.Setenv("PATH", "")
	return filepath.Join(base, "Airlock")
}

// 加锁失败时，serve 以退出码 1 结束，不调用 cabin.New，所以不改写 cabins.json。
// 另一个 Airlock 正在运行但没有取走重新打开的请求时，也以退出码 1 结束。
func TestServeLockFails(t *testing.T) {
	shortReopenWait(t)
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  string
	}{
		{"另一个 Airlock 正在运行", func(t *testing.T, dir string) {
			held, err := lockDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { held.Close() })
		}, "Airlock 已经在运行"},
		// 锁文件的位置是一个目录，打开失败，但不是因为另一个进程持有锁。
		{"不能打开锁文件", func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "airlock.lock"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, "不能锁住数据目录"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := serveDir(t)
			tt.setup(t, dir)
			var stdout, stderr bytes.Buffer
			if code := serve(&stdout, &stderr); code != 1 {
				t.Fatalf("code = %d, want 1\nstderr = %q", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("stderr = %q，want 含 %q", stderr.String(), tt.want)
			}
			if _, err := os.Stat(filepath.Join(dir, "cabins.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("cabins.json：%v", err)
			}
		})
	}
}
