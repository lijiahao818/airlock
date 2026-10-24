package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// start 启动进程后立即返回。程序不存在时返回错误。
func TestStart(t *testing.T) {
	// 运行测试程序自己，不执行任何测试，马上退出。
	if err := start(exec.Command(os.Args[0], "-test.run=^$")); err != nil {
		t.Fatal(err)
	}
	if err := start(exec.Command(filepath.Join(t.TempDir(), "no-such-program"))); err == nil {
		t.Fatal("程序不存在时应该返回错误")
	}
}

// recordStart 替换 start，记录要启动的命令，不启动真实进程。
func recordStart(t *testing.T) *[]*exec.Cmd {
	t.Helper()
	var got []*exec.Cmd
	old := start
	start = func(c *exec.Cmd) error { got = append(got, c); return nil }
	t.Cleanup(func() { start = old })
	return &got
}

func TestOpenURLRejectsOtherInput(t *testing.T) {
	got := recordStart(t)
	for _, raw := range []string{
		"",
		"file:///C:/Windows/System32/calc.exe",
		"javascript:alert(1)",
		"ms-settings:privacy",
		"calc.exe",
		`C:\Windows\System32\calc.exe`,
		"http://",
		"http://user:fake-pass@127.0.0.1/",
		"http://127.0.0.1/a b",
		"http://127.0.0.1/\"&calc",
		"http://127.0.0.1/\x00",
		`\\attacker\share`,
	} {
		if err := OpenURL(raw); err == nil {
			t.Errorf("OpenURL(%q) 应该返回错误", raw)
		}
	}
	if len(*got) != 0 {
		t.Fatalf("拒绝时不能启动进程，启动了 %d 个", len(*got))
	}
}

func TestOpenTerminalRejectsEmptyArgv(t *testing.T) {
	got := recordStart(t)
	for _, argv := range [][]string{nil, {}, {""}} {
		if err := OpenTerminal(argv); err == nil {
			t.Errorf("OpenTerminal(%q) 应该返回错误", argv)
		}
	}
	if len(*got) != 0 {
		t.Fatalf("拒绝时不能启动进程，启动了 %d 个", len(*got))
	}
}
