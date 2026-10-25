package cabin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lijiahao818/airlock/internal/sandbox"
)

// 启动失败时不留下审计记录文件。
func TestFailedStartLeavesNoRunLog(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.create(t, "claude-code")
	e.b.createErr = errors.New("fake: 镜像构建失败")
	if err := e.m.Start(context.Background(), c.ID); err == nil {
		t.Fatal("Create 失败时 Start 应该失败")
	}
	entries, _ := os.ReadDir(filepath.Join(e.dataDir, "runs"))
	if len(entries) != 0 {
		t.Fatalf("runs 目录中有 %d 个文件", len(entries))
	}
}

// 启动期间 List 标出正在启动，刷新后的运行页靠它继续显示进度。
func TestListShowsStarting(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.create(t, "claude-code")
	entered, release := make(chan struct{}), make(chan struct{})
	e.b.onCreate = func(sandbox.Spec) { close(entered); <-release }
	done := make(chan error, 1)
	go func() { done <- e.m.Start(context.Background(), c.ID) }()
	<-entered
	list, _ := e.m.List()
	if !list[0].Starting || list[0].Status != Idle {
		t.Errorf("启动期间 %+v", list[0])
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	list, _ = e.m.List()
	if list[0].Starting || list[0].Status != Running {
		t.Errorf("启动后 %+v", list[0])
	}
	// Starting 不保存到 cabins.json。
	data, _ := os.ReadFile(filepath.Join(e.dataDir, "cabins.json"))
	if string(data) == "" || strings.Contains(string(data), `"starting"`) {
		t.Errorf("cabins.json = %s", data)
	}
}
