package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// shortReopenWait 缩短第二个进程等待请求被取走的时间。
func shortReopenWait(t *testing.T) {
	t.Helper()
	saved := reopenWait
	reopenWait = 200 * time.Millisecond
	t.Cleanup(func() { reopenWait = saved })
}

// watch 在后台运行 watchReopen，返回 open 被调用的次数。测试结束时停止。
func watch(t *testing.T, dir string) *atomic.Int32 {
	t.Helper()
	var opened atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watchReopen(ctx, dir, 10*time.Millisecond, func() { opened.Add(1) })
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return &opened
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
	if got := opened.Load(); got != 1 {
		t.Fatalf("打开了 %d 次，want 1", got)
	}
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
	opened := watch(t, dir)
	for range 100 {
		if gone(t, dir) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := opened.Load(); got != 1 {
		t.Fatalf("打开了 %d 次，want 1", got)
	}
}

// reopenGap 内的重复请求被取走，但不再打开浏览器。
func TestReopenGap(t *testing.T) {
	dir := t.TempDir()
	opened := watch(t, dir)
	for range 3 {
		if !requestReopen(dir, 2*time.Second, 10*time.Millisecond) {
			t.Fatal("请求没有被取走")
		}
	}
	if got := opened.Load(); got != 1 {
		t.Fatalf("%v 内打开了 %d 次，want 1", reopenGap, got)
	}
}

// 没有实例取走请求时，requestReopen 超时，删除请求文件。数据目录不能写入时直接返回 false。
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

// #92 AC-1、AC-3：Airlock 已经在运行时，再次运行 airlock 会请正在运行的实例重新打开控制台，
// 以退出码 0 结束。输出中没有令牌，也不会改写 cabins.json。
func TestServeReopens(t *testing.T) {
	shortReopenWait(t)
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
	if !strings.Contains(stdout.String(), "已在浏览器中重新打开控制台") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if out := stdout.String() + stderr.String(); strings.Contains(out, "token") || strings.Contains(out, "http") {
		t.Fatalf("输出中有网址或令牌：%q", out)
	}
	if got := opened.Load(); got != 1 {
		t.Fatalf("打开了 %d 次，want 1", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "cabins.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cabins.json：%v", err)
	}
}
