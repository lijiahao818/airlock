package desktop

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// roundTrip 按 os/exec 在 Windows 上的规则拼出命令行，再按 CommandLineToArgvW 的规则拆开。
func roundTrip(t *testing.T, args []string) []string {
	t.Helper()
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = syscall.EscapeArg(a)
	}
	line, err := syscall.UTF16PtrFromString(strings.Join(quoted, " "))
	if err != nil {
		t.Fatal(err)
	}
	var n int32
	argv, err := syscall.CommandLineToArgv(line, &n)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.LocalFree(syscall.Handle(uintptr(unsafe.Pointer(argv))))
	out := make([]string, n)
	for i := range out {
		out[i] = syscall.UTF16ToString((*argv[i])[:])
	}
	return out
}

func TestOpenURLCommand(t *testing.T) {
	got := recordStart(t)
	u := "http://127.0.0.1:8080/?a=1&b=2|3#token=fake-token"
	if err := OpenURL(u); err != nil {
		t.Fatal(err)
	}
	c := (*got)[0]
	want := []string{filepath.Join(os.Getenv("SystemRoot"), "System32", "rundll32.exe"), "url.dll,FileProtocolHandler", u}
	if !reflect.DeepEqual(c.Args, want) || c.Path != want[0] {
		t.Fatalf("got %q, want %q", c.Args, want)
	}
	// 网址不含空白和引号，所以 rundll32 收到的就是网址本身，没有多余的引号。
	if q := syscall.EscapeArg(u); q != u {
		t.Fatalf("网址被加上了引号：%s", q)
	}
}

func TestOpenTerminalPassesArgsAsIs(t *testing.T) {
	got := recordStart(t)
	argv := []string{"docker", "exec", "-it", "a b", `say "hi"`, "x&calc", "y|calc", `trail\`, "%PATH%", "^"}
	if err := OpenTerminal(argv); err != nil {
		t.Fatal(err)
	}
	c := (*got)[0]
	if !reflect.DeepEqual(c.Args, argv) {
		t.Fatalf("got %q", c.Args)
	}
	if c.SysProcAttr == nil || c.SysProcAttr.CreationFlags&createNewConsole == 0 || c.SysProcAttr.CmdLine != "" {
		t.Fatalf("必须打开新的控制台，不能自定义命令行：%+v", c.SysProcAttr)
	}
	if strings.EqualFold(filepath.Base(c.Path), "cmd.exe") {
		t.Fatal("不能经过 cmd.exe")
	}
	if back := roundTrip(t, c.Args); !reflect.DeepEqual(back, argv) {
		t.Fatalf("目标程序收到 %q，want %q", back, argv)
	}
}

func TestParsePicked(t *testing.T) {
	for out, want := range map[string]string{"": "", "\r\n": "", `C:\code\演示 项目` + "\r\n": `C:\code\演示 项目`} {
		if got, err := parsePicked(out); err != nil || got != want {
			t.Errorf("parsePicked(%q) = %q, %v", out, got, err)
		}
	}
	for _, bad := range []string{`code\demo`, "错误"} {
		if _, err := parsePicked(bad); err == nil {
			t.Errorf("parsePicked(%q) 应该返回错误", bad)
		}
	}
}

func TestPickFolderCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PickFolder(ctx); err == nil {
		t.Fatal("ctx 已取消时应该返回错误")
	}
}

// 找不到系统目录时拒绝，不从 PATH 或当前目录查找 rundll32 和 powershell。
func TestRejectsMissingSystemRoot(t *testing.T) {
	for _, root := range []string{"", "Windows"} {
		t.Setenv("SystemRoot", root)
		got := recordStart(t)
		if err := OpenURL("http://127.0.0.1/"); err == nil {
			t.Errorf("SystemRoot=%q 时 OpenURL 应该返回错误", root)
		}
		if len(*got) != 0 {
			t.Errorf("SystemRoot=%q 时不能启动进程", root)
		}
		if _, err := PickFolder(context.Background()); err == nil {
			t.Errorf("SystemRoot=%q 时 PickFolder 应该返回错误", root)
		}
	}
}
