package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/lijiahao818/airlock/internal/desktop"
	"github.com/lijiahao818/airlock/internal/sandbox"
	"github.com/lijiahao818/airlock/internal/vault"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0:
		return serve(stdout, stderr)
	case len(args) == 1 && args[0] == "version":
		fmt.Fprintln(stdout, version)
		return 0
	}
	fmt.Fprintln(stderr, "用法：airlock 启动控制台；airlock version 输出版本号")
	return 2
}

// serve 启动控制台和网页终端，用浏览器打开控制台，直到用户关闭窗口或按 Ctrl+C。
// 输出中不含控制台网址，因为网址中有令牌。
func serve(stdout, stderr io.Writer) int {
	// Windows 上 UserCacheDir 是 %LOCALAPPDATA%（docs/architecture.md 第 7 节）。
	base, err := os.UserCacheDir()
	if err != nil {
		fmt.Fprintf(stderr, "Airlock 启动失败：找不到数据目录：%v\n", err)
		return 1
	}
	dir := filepath.Join(base, "Airlock")
	if r, err := redirected(base, "Airlock"); blocked(stderr, dir, r, err) {
		return 1
	}
	// 加锁要在 assemble 之前：cabin.New 会删除带 Airlock 标签的全部容器，并改写 cabins.json。
	lock, err := lockDir(dir)
	if errors.Is(err, errHeld) {
		fmt.Fprintln(stderr, "Airlock 已经在运行。请切换到已经打开的控制台标签页。找不到时，先关闭另一个 Airlock 窗口，再重新运行 Airlock。")
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "Airlock 启动失败：不能锁住数据目录：%v\n请确认 %s 可以写入，然后再试一次。\n", err, dir)
		return 1
	}
	defer lock.Close()
	a, err := assemble(dir, sandbox.Docker(), vault.Get)
	if err != nil {
		fmt.Fprintf(stderr, "Airlock 启动失败：%v\n请关闭其他正在运行的 Airlock，然后再试一次。\n", err)
		return 1
	}
	defer a.close()

	// 事件流是长连接，所以只限制读请求头的时间。
	cs := &http.Server{Handler: a.console, ReadHeaderTimeout: 10 * time.Second}
	ts := &http.Server{Handler: a.term, ReadHeaderTimeout: 10 * time.Second}
	defer cs.Close()
	defer ts.Close()
	errc := make(chan error, 2)
	go func() { errc <- cs.Serve(a.consoleLn) }()
	go func() { errc <- ts.Serve(a.termLn) }()

	// Windows 关闭控制台窗口时，Go 收到 SIGTERM。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := desktop.OpenURL(a.url()); err != nil {
		// 错误信息含网址和令牌，所以不输出 err。
		fmt.Fprintln(stderr, "Airlock 打不开浏览器。请检查 Windows 的默认浏览器设置，然后关闭这个窗口，重新运行 Airlock。")
	} else {
		fmt.Fprintln(stdout, "Airlock 控制台已在浏览器中打开。关闭这个窗口或按 Ctrl+C 退出 Airlock。")
	}
	select {
	case <-ctx.Done():
		fmt.Fprintln(stdout, "Airlock 正在停止运行中的工作舱……")
		if err := stopCabins(a.cabins, shutdownTimeout); err != nil {
			fmt.Fprintf(stderr, "有沙箱没有停止：%v\n下次运行 Airlock 时会删除它们，这些工作舱会进入出舱审查。\n", err)
		}
		return 0
	case err := <-errc:
		fmt.Fprintf(stderr, "Airlock 停止运行：%v\n请重新运行 Airlock。\n", err)
		return 1
	}
}

// shutdownTimeout 是退出时停止沙箱的时限。关闭控制台窗口时，Windows 大约 5 秒后强制结束进程，
// 按 Ctrl+C 时没有这个限制。没有停完的沙箱由下次启动时的 Prune 删除。
const shutdownTimeout = 30 * time.Second

// stopCabins 停止全部运行中的工作舱。Docker Desktop 不响应时，最多等待 timeout，程序仍然可以退出。
func stopCabins(m interface{ StopAll(context.Context) error }, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.StopAll(ctx) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
