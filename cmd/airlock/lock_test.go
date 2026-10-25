package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
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
	shortReopen(t)
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

// lockedBuffer 是可以并发写入的 bytes.Buffer。serve 的协程和测试同时读写输出。
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// serve 启动后一直运行。用户按 Ctrl+C 时，serve 停止工作舱，以退出码 0 结束。
// Windows 上 serve 会打开真实的浏览器，测试也不能给自己发 Ctrl+C，所以只在其他系统上运行。
func TestServeRunsUntilInterrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上会打开真实的浏览器")
	}
	serveDir(t)
	var stdout, stderr lockedBuffer
	code := make(chan int, 1)
	go func() { code <- serve(&stdout, &stderr) }()
	// serve 先注册信号处理，再打开浏览器。这个系统打不开浏览器，看到提示后才能安全地发信号。
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(stderr.String(), "打不开浏览器") {
		select {
		case c := <-code:
			t.Fatalf("serve 提前退出：code = %d，stderr = %q", c, stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("serve 没有进入等待：stderr = %q", stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-code:
		if c != 0 {
			t.Fatalf("code = %d，want 0；stderr = %q", c, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("收到 Ctrl+C 后 serve 没有退出")
	}
	if !strings.Contains(stdout.String(), "正在停止运行中的工作舱") {
		t.Errorf("stdout = %q，应说明正在停止工作舱", stdout.String())
	}
}
