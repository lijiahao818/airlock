package main

import (
	"context"
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
	if r, err := redirected(base, dir); err != nil {
		fmt.Fprintf(stderr, "Airlock 启动失败：数据目录 %s 不能写入：%v\n请检查这个目录的权限，然后再试一次。\n", dir, err)
		return 1
	} else if r {
		fmt.Fprintln(stderr, "Airlock 不能在这个窗口中运行：这个窗口属于另一个应用（例如 Claude 桌面应用），Windows 会把 Airlock 的数据转存到那个应用的私有目录，沙箱因此看不到你的项目文件。\n请关闭这个窗口，在资源管理器中双击 airlock.exe 启动 Airlock。")
		return 1
	}
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
		return 0
	case err := <-errc:
		fmt.Fprintf(stderr, "Airlock 停止运行：%v\n请重新运行 Airlock。\n", err)
		return 1
	}
}
