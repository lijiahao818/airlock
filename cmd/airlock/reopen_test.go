package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// shortReopen 缩短第二个进程的等待时间和两次打开的间隔。
func shortReopen(t *testing.T) {
	t.Helper()
	wait, gap := reopenWait, reopenGap
	reopenWait, reopenGap = time.Second, 200*time.Millisecond
	t.Cleanup(func() { reopenWait, reopenGap = wait, gap })
}

// watch 在后台运行 watchReopen。每次打开控制台时，向返回的通道发送当时的时间。测试结束时停止。
func watch(t *testing.T, dir string) <-chan time.Time {
	t.Helper()
	opened := make(chan time.Time, 10)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watchReopen(ctx, dir, 10*time.Millisecond, func() { opened <- time.Now() })
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return opened
}

// waitOpen 等待控制台被打开一次。watchReopen 先删除请求文件，再打开控制台，所以要等信号，不能只看文件。
func waitOpen(t *testing.T, opened <-chan time.Time) time.Time {
	t.Helper()
	select {
	case at := <-opened:
		return at
	case <-time.After(2 * time.Second):
		t.Fatal("没有打开控制台")
		return time.Time{}
	}
}

// noOpen 确认 d 内没有再打开控制台。
func noOpen(t *testing.T, opened <-chan time.Time, d time.Duration) {
	t.Helper()
	select {
	case <-opened:
		t.Fatal("多打开了一次控制台")
	case <-time.After(d):
	}
}

func gone(t *testing.T, dir string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, reopenFile))
	return errors.Is(err, fs.ErrNotExist)
}

// #92：正在运行的实例取走请求，打开一次控制台。
func TestReopen(t *testing.T) {
	dir := t.TempDir()
	opened := watch(t, dir)
	if !requestReopen(dir, 2*time.Second, 10*time.Millisecond) {
		t.Fatal("请求没有被取走")
	}
	waitOpen(t, opened)
	noOpen(t, opened, 50*time.Millisecond)
	if !gone(t, dir) {
		t.Fatal("请求文件还在")
	}
}

// watchReopen 开始前就已经存在的请求也会处理：第一个实例启动期间，用户可能又运行了一次。
func TestReopenEarlyRequest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, reopenFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitOpen(t, watch(t, dir))
}

// reopenGap 内的请求留在原处，间隔过后才处理，所以第二个进程报告成功时，浏览器确实打开了。
func TestReopenGap(t *testing.T) {
	shortReopen(t)
	dir := t.TempDir()
	opened := watch(t, dir)
	if !requestReopen(dir, reopenWait, 10*time.Millisecond) {
		t.Fatal("第一个请求没有被取走")
	}
	first := waitOpen(t, opened)
	if !requestReopen(dir, reopenWait, 10*time.Millisecond) {
		t.Fatal("第二个请求没有被取走")
	}
	if second := waitOpen(t, opened); second.Sub(first) < reopenGap {
		t.Fatalf("两次打开只间隔 %v，want 至少 %v", second.Sub(first), reopenGap)
	}
}

// 没有实例取走请求时，requestReopen 超时，删除请求文件。数据目录不存在时直接返回 false。
func TestRequestReopenFails(t *testing.T) {
	dir := t.TempDir()
	if requestReopen(dir, 50*time.Millisecond, 10*time.Millisecond) {
		t.Fatal("没有实例时请求不应该成功")
	}
	if !gone(t, dir) {
		t.Fatal("超时后请求文件还在")
	}
	if requestReopen(filepath.Join(dir, "missing"), 50*time.Millisecond, 10*time.Millisecond) {
		t.Fatal("数据目录不存在时请求不应该成功")
	}
}

// reopenConsole 打开失败时输出中文提示，提示中没有 open 返回的错误，因为错误里有网址和令牌。成功时不输出。
func TestReopenConsole(t *testing.T) {
	for _, fail := range []bool{true, false} {
		dir := t.TempDir()
		calls := make(chan struct{}, 1)
		open := func() error {
			calls <- struct{}{}
			if fail {
				return errors.New("fake: 打不开 http://127.0.0.1:1/#token=fake-console-token")
			}
			return nil
		}
		var stderr bytes.Buffer
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			reopenConsole(ctx, dir, 10*time.Millisecond, open, &stderr)
			close(done)
		}()
		ok := requestReopen(dir, 2*time.Second, 10*time.Millisecond)
		<-calls
		cancel()
		<-done
		if !ok {
			t.Fatal("请求没有被取走")
		}
		out := stderr.String()
		if fail && !strings.Contains(out, "打不开浏览器") {
			t.Errorf("打开失败时 stderr = %q", out)
		}
		if !fail && out != "" {
			t.Errorf("打开成功时 stderr = %q", out)
		}
		if strings.Contains(out, "token") || strings.Contains(out, "http") {
			t.Errorf("stderr 中有网址或令牌：%q", out)
		}
	}
}

// #92 AC-1、AC-3：Airlock 已经在运行时，再次运行 airlock 会请正在运行的实例重新打开控制台，
// 以退出码 0 结束。输出中没有令牌，也不会改写 cabins.json。
func TestServeReopens(t *testing.T) {
	shortReopen(t)
	dir := serveDir(t)
	held, err := lockDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { held.Close() })
	opened := watch(t, dir)
	var stdout, stderr bytes.Buffer
	if code := serve(&stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, want 0\nstderr = %q", code, stderr.String())
	}
	waitOpen(t, opened)
	if !strings.Contains(stdout.String(), "已在浏览器中重新打开控制台") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if out := stdout.String() + stderr.String(); strings.Contains(out, "token") || strings.Contains(out, "http") {
		t.Fatalf("输出中有网址或令牌：%q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "cabins.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cabins.json：%v", err)
	}
}
