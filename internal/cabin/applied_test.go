package cabin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// 部分改动写回失败后，已写回的改动不是冲突，可以重试整个审查。重启后报告仍然正确。
func TestApplyAfterPartialFailure(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	files := e.b.lastSpec(t).StagingDir
	writeFile(t, filepath.Join(files, "main.go"), "// agent 的改动\n")
	writeFile(t, filepath.Join(files, "AGENTS.md"), "# agent 的改动\n")
	e.m.Stop(ctx, c.ID)
	writeFile(t, filepath.Join(c.Project, "AGENTS.md"), "# 用户在审查前的改动\n")

	if err := e.m.Apply(c.ID, "main.go"); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Apply(c.ID, "AGENTS.md"); err == nil {
		t.Fatal("冲突的改动不能写回")
	}
	// 重试时再次写回 main.go，返回成功，不修改项目。
	writeFile(t, filepath.Join(c.Project, "main.go"), "// 用户在写回后的改动\n")
	if err := e.m.Apply(c.ID, "main.go"); err != nil {
		t.Fatalf("再次写回 = %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(c.Project, "main.go")); string(data) != "// 用户在写回后的改动\n" {
		t.Fatalf("再次写回修改了项目：%q", data)
	}

	// 重启 Airlock 后，已写回的标记仍然在。
	m2, err := New(Options{DataDir: e.dataDir, Backend: &fakeBackend{}, Secret: e.m.o.Secret})
	if err != nil {
		t.Fatal(err)
	}
	items, err := m2.ReviewItems(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	state := map[string][2]bool{}
	for _, it := range items {
		state[it.Path] = [2]bool{it.Applied, it.Conflict}
	}
	if state["main.go"] != [2]bool{true, false} || state["AGENTS.md"] != [2]bool{false, true} {
		t.Fatalf("重启后 %+v", items)
	}
	if err := m2.FinishReview(c.ID); err != nil {
		t.Fatal(err)
	}
	rep, err := m2.Report(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	applied := map[string]bool{}
	for _, f := range rep.Files {
		applied[f.Path] = f.Applied
	}
	if len(applied) != 2 || !applied["main.go"] || applied["AGENTS.md"] {
		t.Fatalf("Files = %+v", rep.Files)
	}
}

// fakeRemove 让 m 删除暂存区时先失败 fails 次，模拟 Docker Desktop 还占用目录。
// 返回调用次数，读取时持有 m.mu。
func fakeRemove(m *Manager, fails int) *int {
	calls := new(int)
	m.retryDelay = 10 * time.Millisecond
	m.removeAll = func(p string) error {
		*calls++
		if *calls <= fails {
			return errors.New("fake: 目录被另一个进程占用")
		}
		return os.RemoveAll(p)
	}
	return calls
}

func waitGone(t *testing.T, dir string) {
	t.Helper()
	for range 200 {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s 没有被删除", dir)
}

// 停止后立即完成审查时，暂存区可能还被占用。审查仍然成功，暂存区在后台删除。
func TestFinishReviewRetriesRemove(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	e.m.Stop(context.Background(), c.ID)
	calls := fakeRemove(e.m, 2)
	if err := e.m.FinishReview(c.ID); err != nil {
		t.Fatalf("FinishReview = %v", err)
	}
	if e.status(t, c.ID) != Idle {
		t.Fatalf("状态 = %q", e.status(t, c.ID))
	}
	if _, err := e.m.Report(c.ID); err != nil {
		t.Fatalf("Report = %v", err)
	}
	waitGone(t, e.m.areaDir(c.ID))
	e.m.mu.Lock()
	defer e.m.mu.Unlock()
	if *calls != 3 {
		t.Errorf("删除了 %d 次，want 3", *calls)
	}
}

// 后台重试期间用户再次启动工作舱时，重试停止，不删除新的暂存区。
func TestRetryRemoveStopsAfterStart(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	e.m.Stop(context.Background(), c.ID)
	fakeRemove(e.m, 1)
	e.m.retryDelay = 200 * time.Millisecond
	if err := e.m.FinishReview(c.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Start(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(e.b.lastSpec(t).StagingDir); err != nil {
		t.Fatalf("新的暂存区被删除：%v", err)
	}
}

// 启动时删除不属于 Review 状态工作舱的暂存区。
func TestNewPrunesLeftoverAreas(t *testing.T) {
	dir := t.TempDir()
	saved := []Cabin{
		{ID: "review", Name: "a", Project: `C:\a`, Preset: "claude-code", Tier: "standard", Status: Review, RunID: "r1"},
		{ID: "running", Name: "b", Project: `C:\b`, Preset: "claude-code", Tier: "standard", Status: Running, RunID: "r2"},
		{ID: "idle", Name: "c", Project: `C:\c`, Preset: "codex", Tier: "strict", Status: Idle},
	}
	data, _ := json.Marshal(saved)
	writeFile(t, filepath.Join(dir, "cabins.json"), string(data))
	for _, id := range []string{"review", "running", "idle", "orphan"} {
		writeFile(t, filepath.Join(dir, "cabins", id, "files", "x"), "x")
	}
	if _, err := New(Options{DataDir: dir, Backend: &fakeBackend{}, Secret: func(string) (string, error) { return "", nil }}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "cabins"))
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if !slices.Equal(left, []string{"review", "running"}) {
		t.Fatalf("剩下 %v，want [review running]", left)
	}
}
