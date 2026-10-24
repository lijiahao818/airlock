package cabin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// pinDir 让 os.RemoveAll(dir) 失败，测试结束时恢复。Windows 上让 dir 中的一个文件保持打开；
// 其他系统上把 dir 改为只读。root 不受目录权限限制，这时跳过测试。
func pinDir(t *testing.T, dir string) {
	t.Helper()
	pin := filepath.Join(dir, "pinned")
	writeFile(t, pin, "x")
	if runtime.GOOS == "windows" {
		f, err := os.Open(pin)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return
	}
	if os.Geteuid() == 0 {
		t.Skip("root 可以删除只读目录中的文件")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
}

// 项目文件夹在运行中被删除时，列出改动、撤销和完成审查都返回错误，状态不变，也不留下忙标记。
func TestProjectFolderRemoved(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	writeFile(t, filepath.Join(e.b.lastSpec(t).StagingDir, "a.txt"), "a")
	os.RemoveAll(c.Project)
	if _, err := e.m.ReviewItems(c.ID); err == nil {
		t.Fatal("ReviewItems 应该返回错误")
	}
	if err := e.m.Revert(c.ID, "a.txt"); err == nil {
		t.Fatal("Revert 应该返回错误")
	}
	if err := e.m.Stop(ctx, c.ID); err != nil {
		t.Fatalf("Revert 失败后留下了忙标记：%v", err)
	}
	if err := e.m.FinishReview(c.ID); err == nil || e.status(t, c.ID) != Review {
		t.Fatalf("FinishReview = %v，状态 %q", err, e.status(t, c.ID))
	}
}

func TestStartFailsWhenLogsFail(t *testing.T) {
	e := newEnv(t, nil, nil)
	e.b.logsErr = errors.New("fake: docker logs 失败")
	c := e.create(t, "deepseek-harness")
	if err := e.m.Start(context.Background(), c.ID); err == nil {
		t.Fatal("Start 应该失败")
	}
	e.assertCleanedUp(t, c.ID, 1)
}

// 暂存区删不掉时，Start、FinishReview 和 Reset 返回错误，状态不变。
func TestStagingCannotBeRemoved(t *testing.T) {
	ctx := context.Background()
	t.Run("Start 前清理上次的残留", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		c := e.create(t, "claude-code")
		pinDir(t, filepath.Join(e.m.areaDir(c.ID), "files"))
		if err := e.m.Start(ctx, c.ID); err == nil || e.status(t, c.ID) != Idle || e.b.called("create") != 0 {
			t.Fatalf("Start = %v", err)
		}
	})
	t.Run("FinishReview 丢弃暂存区", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		c := e.started(t, "claude-code")
		files := e.b.lastSpec(t).StagingDir
		e.m.Stop(ctx, c.ID)
		pinDir(t, files)
		// 审查记录已经写入，所以回到 Idle，用户不需要重试，审查记录不会重复。
		if err := e.m.FinishReview(c.ID); err == nil || !strings.Contains(err.Error(), "审查已经完成") || e.status(t, c.ID) != Idle {
			t.Fatalf("FinishReview = %v，状态 %q", err, e.status(t, c.ID))
		}
		if err := e.m.FinishReview(c.ID); err == nil {
			t.Fatal("Idle 状态不能再次完成审查")
		}
		if rep, _ := e.m.Report(c.ID); len(rep.Files) != 1 || rep.Files[0].Path != "pinned" {
			t.Fatalf("Files = %+v", rep.Files)
		}
	})
	t.Run("Reset 删除暂存区", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		c := e.started(t, "claude-code")
		files := e.b.lastSpec(t).StagingDir
		e.m.Stop(ctx, c.ID)
		pinDir(t, files)
		if err := e.m.Reset(ctx, c.ID); err == nil || e.status(t, c.ID) != Review {
			t.Fatalf("Reset = %v", err)
		}
	})
}

// 项目中的同一文件在运行期间也被修改时，Apply 拒绝写回，审查记录为没有应用。
func TestApplyConflictIsNotRecorded(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	writeFile(t, filepath.Join(e.b.lastSpec(t).StagingDir, "main.go"), "// agent 的改动\n")
	writeFile(t, filepath.Join(c.Project, "main.go"), "// 用户在运行期间的改动\n")
	e.m.Stop(ctx, c.ID)
	if err := e.m.Apply(c.ID, "main.go"); err == nil {
		t.Fatal("冲突的改动不能写回")
	}
	if data, _ := os.ReadFile(filepath.Join(c.Project, "main.go")); string(data) != "// 用户在运行期间的改动\n" {
		t.Fatalf("用户的改动被覆盖：%q", data)
	}
	if err := e.m.FinishReview(c.ID); err != nil {
		t.Fatal(err)
	}
	rep, _ := e.m.Report(c.ID)
	if len(rep.Files) != 1 || rep.Files[0].Applied {
		t.Fatalf("Files = %+v", rep.Files)
	}
}

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

func TestFinishReviewFailures(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	e.m.Stop(ctx, c.ID)
	os.RemoveAll(e.m.areaDir(c.ID))
	if err := e.m.FinishReview(c.ID); err == nil {
		t.Fatal("暂存区丢失时 FinishReview 应该失败")
	}
	if e.status(t, c.ID) != Review {
		t.Fatal("FinishReview 失败时状态不能改变")
	}
	// 用户可以用 Reset 离开这个状态。
	if err := e.m.Reset(ctx, c.ID); err != nil || e.status(t, c.ID) != Idle {
		t.Fatalf("Reset = %v", err)
	}

	e2 := newEnv(t, nil, nil)
	c2 := e2.started(t, "claude-code")
	e2.m.Stop(ctx, c2.ID)
	// 审计记录的位置是一个目录时，打开失败。
	log := e2.m.runLog(c2.RunID)
	os.Remove(log)
	os.Mkdir(log, 0o755)
	if err := e2.m.FinishReview(c2.ID); err == nil || e2.status(t, c2.ID) != Review {
		t.Fatalf("审计记录打不开时 FinishReview 应该失败：%v", err)
	}
	if _, err := e2.m.Report(c2.ID); err == nil {
		t.Fatal("审计记录不能读取时 Report 应该返回错误")
	}
}

// 程序崩溃留下半行时，报告用之前完整的条目生成。
func TestReportWithHalfLine(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	writeFile(t, filepath.Join(e.b.lastSpec(t).StagingDir, "new.txt"), "x")
	e.m.Stop(ctx, c.ID)
	e.m.FinishReview(c.ID)
	f, _ := os.OpenFile(e.m.runLog(c.RunID), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"gate":{"host":"half`)
	f.Close()
	if rep, err := e.m.Report(c.ID); err != nil || len(rep.Files) != 1 {
		t.Fatalf("Report = %+v, %v", rep, err)
	}
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

// blockSave 让保存工作舱列表失败：临时文件的位置是一个目录。返回的函数恢复保存。
func blockSave(t *testing.T, dataDir string) func() {
	t.Helper()
	tmp := filepath.Join(dataDir, "cabins.json.tmp")
	writeFile(t, filepath.Join(tmp, "x"), "")
	return func() { os.RemoveAll(tmp) }
}

// 保存失败时，操作已经完成，内存中的状态与实际的资源一致，错误信息说明这一点。
func TestSaveFailureAfterStop(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	port := e.b.lastSpec(t).GatePort
	unblock := blockSave(t, e.dataDir)
	err := e.m.Stop(ctx, c.ID)
	if err == nil || !strings.Contains(err.Error(), "操作已经完成") {
		t.Fatalf("Stop = %v", err)
	}
	if e.status(t, c.ID) != Review || listening(port) || e.b.called("remove") != 1 {
		t.Fatal("沙箱已经删除，状态应该是 Review，闸门应该关闭")
	}
	unblock()
	if err := e.m.FinishReview(c.ID); err != nil {
		t.Fatal(err)
	}
}

func TestNewFailsWhenItCannotSave(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "cabins.json"), `[{"id":"c1","status":"running"}]`)
	blockSave(t, dir)
	if _, err := New(Options{DataDir: dir, Backend: &fakeBackend{}, Secret: func(string) (string, error) { return "", nil }}); err == nil {
		t.Fatal("不能保存状态的修改时，New 应该返回错误")
	}
}

func TestWebTokenBadPattern(t *testing.T) {
	e := newEnv(t, nil, nil)
	if _, err := e.m.webToken(context.Background(), "box", "("); err == nil {
		t.Fatal("正则不能编译时应该返回错误")
	}
	// 没有分组时，匹配结果只有一项。不检查会因为取 mt[1] 而崩溃。
	e.b.logs = "token=" + fakeWebToken + "\n"
	for _, p := range []string{`token=\S+`, `(token)=(\S+)`} {
		if _, err := e.m.webToken(context.Background(), "box", p); err == nil {
			t.Errorf("tokenPattern %q 的分组数不对，应该返回错误", p)
		}
	}
}
