package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// reopenFile 是"重新打开控制台"的请求文件，放在数据目录中（#92）。
// 第二个 Airlock 进程拿不到单实例锁时创建它。正在运行的实例取走它，再用浏览器打开控制台。
// 请求文件是空的：令牌只在正在运行的实例的内存中，不经过第二个进程（docs/security.md T-12）。
const reopenFile = "open-console"

// reopenGap 是两次重新打开之间的最短间隔。同一用户的程序反复创建请求时，浏览器不会不停地弹出。
// 间隔内的请求留在原处，间隔过后再处理。它小于 reopenWait，所以第二个进程等得到结果。测试中替换它。
var reopenGap = 2 * time.Second

// reopenWait 是第二个进程等待正在运行的实例取走请求的时间。测试中替换它。
var reopenWait = 3 * time.Second

// requestReopen 创建请求文件，每隔 every 检查一次它是否被取走。
// timeout 内没有被取走时删除它，返回 false，这样正在运行的实例以后不会意外地打开浏览器。
func requestReopen(dir string, timeout, every time.Duration) bool {
	p := filepath.Join(dir, reopenFile)
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		return false
	}
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); {
		time.Sleep(every)
		if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
			return true
		}
	}
	// 删除前的一刻，正在运行的实例可能刚好取走了请求。这时删除失败，也算成功。
	return errors.Is(os.Remove(p), fs.ErrNotExist)
}

// watchReopen 每隔 every 取一次请求文件，取到时调用 open，直到 ctx 结束。
// 删除成功就是取到了请求，不需要另外加锁。上次残留的请求由 serve 在拿到锁后删除。
func watchReopen(ctx context.Context, dir string, every time.Duration, open func()) {
	p := filepath.Join(dir, reopenFile)
	t := time.NewTicker(every)
	defer t.Stop()
	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if time.Since(last) < reopenGap || os.Remove(p) != nil {
			continue
		}
		last = time.Now()
		open()
	}
}

// reopenConsole 处理重新打开控制台的请求，直到 ctx 结束。open 打开控制台，失败时在 stderr 输出提示。
// open 的错误信息含网址和令牌，所以不输出它。
func reopenConsole(ctx context.Context, dir string, every time.Duration, open func() error, stderr io.Writer) {
	watchReopen(ctx, dir, every, func() {
		if open() != nil {
			fmt.Fprintln(stderr, "Airlock 打不开浏览器，没有重新打开控制台。请检查 Windows 的默认浏览器设置。")
		}
	})
}
