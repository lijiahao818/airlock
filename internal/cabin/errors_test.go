package cabin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// 订阅者不读取事件时，publish 丢弃事件，不阻塞闸门。
func TestPublishDoesNotBlock(t *testing.T) {
	e := newEnv(t, nil, nil)
	_, cancel := e.m.Subscribe()
	defer cancel()
	done := make(chan struct{})
	go func() {
		e.m.mu.Lock()
		for range subBuffer + 10 {
			e.m.publish(Event{Kind: "request"})
		}
		e.m.mu.Unlock()
		close(done)
	}()
	<-done
}

func TestStartFailsWhenAuditLogCannotOpen(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.create(t, "claude-code")
	writeFile(t, filepath.Join(e.dataDir, "runs"), "不是目录")
	if err := e.m.Start(context.Background(), c.ID); err == nil {
		t.Fatal("Start 应该失败")
	}
	e.assertCleanedUp(t, c.ID, 0)
}

// cabins.json 被手动改坏时，Start 拒绝无效的预设和档位。
func TestStartRejectsBadSavedCabin(t *testing.T) {
	for name, edit := range map[string]func(*Cabin){
		"预设无效": func(c *Cabin) { c.Preset = "no-such-agent" },
		"档位无效": func(c *Cabin) { c.Tier = "open" },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, nil, nil)
			c := e.create(t, "claude-code")
			e.m.mu.Lock()
			edit(&e.m.cabins[0])
			e.m.mu.Unlock()
			if err := e.m.Start(context.Background(), c.ID); err == nil {
				t.Fatal("Start 应该失败")
			}
			e.assertCleanedUp(t, c.ID, 0)
		})
	}
}

func TestTerminalErrors(t *testing.T) {
	e := newEnv(t, nil, nil)
	e.b.logs = dshLogs
	c := e.started(t, "deepseek-harness")
	// web 型的运行也有终端令牌，但没有终端可以连接。
	e.m.mu.Lock()
	token := e.m.runs[c.ID].token
	e.m.mu.Unlock()
	if _, err := e.m.Attach(context.Background(), c.ID, token); err == nil || e.b.called("exec") != 0 {
		t.Fatalf("web 型 agent 不能 Attach：%v", err)
	}

	e2 := newEnv(t, nil, nil)
	e2.b.noAttach = true
	c2 := e2.started(t, "claude-code")
	if err := e2.m.OpenTerminal(c2.ID); err == nil {
		t.Fatal("没有连接命令时 OpenTerminal 应该返回错误")
	}
}

func TestNewFailures(t *testing.T) {
	secret := func(string) (string, error) { return "", nil }
	file := filepath.Join(t.TempDir(), "data")
	writeFile(t, file, "")
	if _, err := New(Options{DataDir: file, Backend: &fakeBackend{}, Secret: secret}); err == nil {
		t.Error("DataDir 是文件时 New 应该返回错误")
	}
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "cabins.json"), 0o755)
	if _, err := New(Options{DataDir: dir, Backend: &fakeBackend{}, Secret: secret}); err == nil {
		t.Error("cabins.json 不能读取时 New 应该返回错误")
	}
}

func TestWebTokenBadPattern(t *testing.T) {
	e := newEnv(t, nil, nil)
	if _, err := e.m.webToken(context.Background(), "box", "("); err == nil {
		t.Fatal("正则不能编译时应该返回错误")
	}
}
