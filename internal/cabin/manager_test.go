package cabin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNewRejectsBadOptions(t *testing.T) {
	secret := func(string) (string, error) { return fakeSecret, nil }
	for name, o := range map[string]Options{
		"没有 Backend":    {DataDir: t.TempDir(), Secret: secret},
		"没有 Secret":     {DataDir: t.TempDir(), Backend: &fakeBackend{}},
		"DataDir 是相对路径": {DataDir: "airlock-data", Backend: &fakeBackend{}, Secret: secret},
	} {
		if _, err := New(o); err == nil {
			t.Errorf("%s：New 应该返回错误", name)
		}
	}
}

func TestNewRejectsCorruptList(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "cabins.json"), "{不是 JSON")
	if _, err := New(Options{DataDir: dir, Backend: &fakeBackend{}, Secret: func(string) (string, error) { return "", nil }}); err == nil {
		t.Fatal("cabins.json 损坏时应该返回错误")
	}
}

func TestCreateAndPersist(t *testing.T) {
	e := newEnv(t, nil, nil)
	project := newProject(t)
	c, err := e.m.Create(project, "claude-code", "standard")
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != Idle || c.Name != filepath.Base(project) || c.Project != project || c.ID == "" || c.RunID != "" {
		t.Fatalf("Create = %+v", c)
	}
	// 重启 Manager 后，List 返回同样的工作舱。
	m2, err := New(Options{DataDir: e.dataDir, Backend: &fakeBackend{}, Secret: e.m.o.Secret})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := m2.List()
	if !reflect.DeepEqual(list, []Cabin{c}) {
		t.Fatalf("重启后 List = %+v, want %+v", list, []Cabin{c})
	}
}

func TestCreateRejectsBadInput(t *testing.T) {
	e := newEnv(t, nil, nil)
	project := newProject(t)
	file := filepath.Join(project, "main.go")
	inData := filepath.Join(e.dataDir, "sub")
	os.Mkdir(inData, 0o755)
	cases := map[string][3]string{
		"相对路径":   {"code/demo", "claude-code", "standard"},
		"文件夹不存在": {filepath.Join(project, "no-such-dir"), "claude-code", "standard"},
		"是文件":    {file, "claude-code", "standard"},
		"未知预设":   {project, "no-such-agent", "standard"},
		"未知档位":   {project, "claude-code", "open"},
		"在数据目录中": {inData, "claude-code", "standard"},
		"包含数据目录": {filepath.Dir(e.dataDir), "claude-code", "standard"},
		"就是数据目录": {e.dataDir, "claude-code", "standard"},
	}
	for name, in := range cases {
		if _, err := e.m.Create(in[0], in[1], in[2]); err == nil {
			t.Errorf("%s：Create 应该返回错误", name)
		}
	}
	if list, _ := e.m.List(); len(list) != 0 {
		t.Fatalf("拒绝后不能新建工作舱：%+v", list)
	}
}

func TestCreateKeepsListWhenSaveFails(t *testing.T) {
	e := newEnv(t, nil, nil)
	// cabins.json 的位置是一个目录时，改名失败。
	os.Mkdir(filepath.Join(e.dataDir, "cabins.json"), 0o755)
	writeFile(t, filepath.Join(e.dataDir, "cabins.json", "x"), "")
	if _, err := e.m.Create(newProject(t), "claude-code", "standard"); err == nil {
		t.Fatal("保存失败时 Create 应该返回错误")
	}
	if list, _ := e.m.List(); len(list) != 0 {
		t.Fatalf("保存失败时列表不能改变：%+v", list)
	}
}

// New 先调用 Prune，再把 Running 改为 Review。Prune 失败时继续启动。
func TestNewPrunesThenMovesRunningToReview(t *testing.T) {
	dir := t.TempDir()
	saved := []Cabin{
		{ID: "c1", Name: "a", Project: `C:\a`, Preset: "claude-code", Tier: "standard", Status: Running, RunID: "r1"},
		{ID: "c2", Name: "b", Project: `C:\b`, Preset: "codex", Tier: "strict", Status: Idle},
	}
	data, _ := json.Marshal(saved)
	writeFile(t, filepath.Join(dir, "cabins.json"), string(data))
	b := &fakeBackend{}
	var statusAtPrune Status
	b.onPrune = func() {
		var got []Cabin
		data, _ := os.ReadFile(filepath.Join(dir, "cabins.json"))
		json.Unmarshal(data, &got)
		statusAtPrune = got[0].Status
	}
	m, err := New(Options{DataDir: dir, Backend: b, Secret: func(string) (string, error) { return "", nil }})
	if err != nil {
		t.Fatal(err)
	}
	if statusAtPrune != Running {
		t.Fatalf("Prune 时状态已经是 %q，应该先 Prune 再修改状态", statusAtPrune)
	}
	list, _ := m.List()
	if list[0].Status != Review || list[0].RunID != "r1" || list[1].Status != Idle {
		t.Fatalf("List = %+v", list)
	}
	// 修改已经保存，再次重启仍然是 Review。
	m2, _ := New(Options{DataDir: dir, Backend: &fakeBackend{}, Secret: m.o.Secret})
	if list, _ := m2.List(); list[0].Status != Review {
		t.Fatalf("重启后 = %+v", list)
	}
}

func TestCheckDocker(t *testing.T) {
	e := newEnv(t, nil, nil)
	if err := e.m.CheckDocker(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.b.checkErr = errors.New("fake: Docker Desktop 没有运行")
	if err := e.m.CheckDocker(context.Background()); err != e.b.checkErr {
		t.Fatalf("CheckDocker = %v", err)
	}
}

func TestUnknownCabin(t *testing.T) {
	e := newEnv(t, nil, nil)
	ctx := context.Background()
	for name, err := range map[string]error{
		"Start":         e.m.Start(ctx, "nope"),
		"Stop":          e.m.Stop(ctx, "nope"),
		"Reset":         e.m.Reset(ctx, "nope"),
		"OpenTerminal":  e.m.OpenTerminal("nope"),
		"Revert":        e.m.Revert("nope", "a"),
		"Apply":         e.m.Apply("nope", "a"),
		"FinishReview":  e.m.FinishReview("nope"),
		"ResetDecision": e.m.ResetDecision("nope", "evil.example"),
	} {
		if err == nil {
			t.Errorf("%s 对未知工作舱应该返回错误", name)
		}
	}
	if _, err := e.m.AgentURL("nope"); err == nil {
		t.Error("AgentURL 对未知工作舱应该返回错误")
	}
	if _, err := e.m.Attach(ctx, "nope", "x"); err == nil {
		t.Error("Attach 对未知工作舱应该返回错误")
	}
	if _, err := e.m.EntryFindings("nope"); err == nil {
		t.Error("EntryFindings 对未知工作舱应该返回错误")
	}
	if _, err := e.m.ReviewItems("nope"); err == nil {
		t.Error("ReviewItems 对未知工作舱应该返回错误")
	}
	if _, err := e.m.Report("nope"); err == nil {
		t.Error("Report 对未知工作舱应该返回错误")
	}
}

func TestSubscribeCancel(t *testing.T) {
	e := newEnv(t, nil, nil)
	ch, cancel := e.m.Subscribe()
	cancel()
	cancel() // 重复调用不出错
	if _, ok := <-ch; ok {
		t.Fatal("取消后通道应该关闭")
	}
}
