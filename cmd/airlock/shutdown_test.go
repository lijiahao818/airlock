package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeStopper func(ctx context.Context) error

func (f fakeStopper) StopAll(ctx context.Context) error { return f(ctx) }

func TestStopCabins(t *testing.T) {
	if err := stopCabins(fakeStopper(func(context.Context) error { return nil }), time.Second); err != nil {
		t.Fatalf("正常停止 = %v", err)
	}
	// StopAll 不理会 ctx 时，stopCabins 也在超时后返回。
	block := make(chan struct{})
	defer close(block)
	start := time.Now()
	err := stopCabins(fakeStopper(func(context.Context) error { <-block; return nil }), 50*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("超时 = %v，用了 %v", err, time.Since(start))
	}
}

func TestWait(t *testing.T) {
	ok := func(context.Context) error { return nil }
	tests := []struct {
		name    string
		quit    bool  // 用户关闭窗口或按 Ctrl+C
		serve   error // 控制台或网页终端停止服务的原因
		stop    func(context.Context) error
		code    int
		stdout  string
		stderr  string
		stopped bool
	}{
		{"用户退出", true, nil, ok, 0, "正在停止", "", true},
		{"有沙箱没有停止", true, nil, func(context.Context) error { return errors.New("fake: 删除失败") }, 0, "正在停止", "有沙箱没有停止", true},
		{"服务出错", false, errors.New("fake: 监听失败"), ok, 1, "", "停止运行", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.quit {
				cancel()
			}
			errc := make(chan error, 1)
			if tt.serve != nil {
				errc <- tt.serve
			}
			stopped := false
			cabins := fakeStopper(func(ctx context.Context) error { stopped = true; return tt.stop(ctx) })
			var stdout, stderr bytes.Buffer
			if code := wait(ctx, errc, cabins, &stdout, &stderr); code != tt.code {
				t.Fatalf("code = %d, want %d", code, tt.code)
			}
			if !strings.Contains(stdout.String(), tt.stdout) || !strings.Contains(stderr.String(), tt.stderr) {
				t.Fatalf("stdout = %q\nstderr = %q", stdout.String(), stderr.String())
			}
			if stopped != tt.stopped {
				t.Fatalf("调用 StopAll = %v，want %v", stopped, tt.stopped)
			}
		})
	}
}
