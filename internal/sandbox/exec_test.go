package sandbox

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAttachCommand(t *testing.T) {
	got := Docker().AttachCommand("airlock-fake-cabin-box", []string{"claude"})
	want := []string{"docker", "exec", "-it", "-e", "TERM=xterm-256color", "-e", "LANG=C.UTF-8", "airlock-fake-cabin-box", "tmux", "new-session", "-A", "-s", "agent", "claude"}
	if !slices.Equal(got, want) {
		t.Errorf("AttachCommand = %v", got)
	}
	if got := Docker().AttachCommand("fake-other", nil); got != nil {
		t.Errorf("不合法的 ID 返回了 %v", got)
	}
}

func TestExecArgs(t *testing.T) {
	args := execArgs("airlock-fake-cabin-box", []string{"codex", "it's; rm -rf /"})
	inner := args[len(args)-2]
	if !strings.HasPrefix(inner, "stty eof undef; ") {
		t.Errorf("连接前没有去掉结束符：%s", inner)
	}
	if !strings.HasSuffix(inner, `exec tmux new-session -A -s agent 'codex' 'it'\''s; rm -rf /'`) {
		t.Errorf("命令没有正确加引号：%s", inner)
	}
	if !slices.Equal(args[:8], []string{"exec", "-i", "-e", "TERM=xterm-256color", "-e", "LANG=C.UTF-8", "airlock-fake-cabin-box", "script"}) {
		t.Errorf("Exec 的参数 %v", args)
	}
	if inner := execArgs("airlock-fake-cabin-box", nil)[9]; !strings.HasSuffix(inner, "-s agent") {
		t.Errorf("没有命令时 %s", inner)
	}
}

func TestExecSession(t *testing.T) {
	f := &fakeDocker{}
	sess, err := f.backend().Exec(context.Background(), "airlock-fake-cabin-box", []string{"sh"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if _, err := io.WriteString(sess, "fake-input\n"); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("fake-input\n"))
	if _, err := io.ReadFull(sess, buf); err != nil || string(buf) != "fake-input\n" {
		t.Fatalf("读到 %q, %v；终端设备名那一行应当被去掉", buf, err)
	}
	if err := sess.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	if _, args := f.call(t, "exec", "airlock-fake-cabin-box", "stty"); !slices.Equal(args, []string{"exec", "airlock-fake-cabin-box", "stty", "-F", "/dev/pts/7", "cols", "120", "rows", "40"}) {
		t.Errorf("Resize 的命令 %v", args)
	}
	for _, size := range [][2]int{{0, 40}, {120, 0}, {1001, 40}} {
		if err := sess.Resize(size[0], size[1]); err == nil {
			t.Errorf("Resize(%d, %d) 没有报错", size[0], size[1])
		}
	}
	if err := sess.Close(); err != nil {
		t.Fatal(err)
	}
	if _, args := f.call(t, "exec", "airlock-fake-cabin-box", "tmux"); !slices.Equal(args, []string{"exec", "airlock-fake-cabin-box", "tmux", "detach-client", "-t", "/dev/pts/7"}) {
		t.Errorf("Close 的命令 %v", args)
	}
	n := len(f.calls)
	sess.Close()
	if len(f.calls) != n {
		t.Error("第二次 Close 又执行了 docker 命令")
	}
}

// 终端设备名来自沙箱，格式不对时 Exec 报错，不把它拼进命令。
func TestExecRejectsBadTTY(t *testing.T) {
	setHelperMode(t, "bad-tty")
	f := &fakeDocker{}
	if _, err := f.backend().Exec(context.Background(), "airlock-fake-cabin-box", nil); err == nil {
		t.Fatal("终端设备名格式不对时 Exec 没有报错")
	}
	if len(f.calls) != 0 {
		t.Errorf("执行了 docker 命令 %v", f.calls)
	}
}

func TestExecTimeout(t *testing.T) {
	setHelperMode(t, "silent")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := (&fakeDocker{}).backend().Exec(ctx, "airlock-fake-cabin-box", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Exec = %v，want 超时", err)
	}
}

func TestExecRejectsBadID(t *testing.T) {
	if _, err := (&fakeDocker{}).backend().Exec(context.Background(), "fake-other", nil); err == nil {
		t.Fatal("不合法的 ID 没有报错")
	}
}

// 关闭输入后命令迟迟不退出时，Close 在 3 秒后结束它。
func TestCloseKillsStuckCommand(t *testing.T) {
	setHelperMode(t, "stuck")
	sess, err := (&fakeDocker{}).backend().Exec(context.Background(), "airlock-fake-cabin-box", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	sess.Close()
	if d := time.Since(start); d < 3*time.Second || d > 10*time.Second {
		t.Fatalf("Close 用了 %v，want 约 3 秒", d)
	}
	if sess.(*session).cmd.ProcessState == nil {
		t.Fatal("Close 之后命令仍在运行")
	}
}

func TestLogs(t *testing.T) {
	d := (&fakeDocker{}).backend()
	ctx, cancel := context.WithCancel(context.Background())
	r, err := d.Logs(ctx, "airlock-fake-cabin-box")
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("fake-log-line\n"))
	if _, err := io.ReadFull(r, buf); err != nil || string(buf) != "fake-log-line\n" {
		t.Fatalf("Logs 读到 %q, %v", buf, err)
	}
	// ctx 结束时 docker logs 结束，读取方收到错误。
	cancel()
	done := make(chan error, 1)
	go func() { _, err := io.ReadAll(r); done <- err }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ctx 结束后 Logs 没有结束")
	}
	r.Close()

	r, err = d.Logs(context.Background(), "airlock-fake-cabin-box")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Logs(context.Background(), "fake-other"); err == nil {
		t.Fatal("不合法的 ID 没有报错")
	}
}
