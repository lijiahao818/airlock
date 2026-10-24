package desktop

import (
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
