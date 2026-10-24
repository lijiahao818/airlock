package cabin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lijiahao818/airlock/internal/sandbox"
	"github.com/lijiahao818/airlock/internal/staging"
)

// 测试只用 fake- 前缀的假密钥。
const fakeSecret = "fake-real-secret-do-not-leak"

// fakeBackend 记录调用，不调用 docker。
type fakeBackend struct {
	mu        sync.Mutex
	calls     []string
	specs     []sandbox.Spec
	execs     [][]string
	logs      string
	noAttach  bool // AttachCommand 返回空，模拟 ID 不是 sandbox 创建的沙箱
	checkErr  error
	createErr error
	removeErr error
	cacheErr  error
	onCreate  func(s sandbox.Spec) // Create 时调用，用来检查启动顺序或阻塞
	onPrune   func()
}

var _ sandbox.Backend = (*fakeBackend)(nil)

func (f *fakeBackend) record(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

func (f *fakeBackend) called(call string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == call {
			n++
		}
	}
	return n
}

func (f *fakeBackend) lastSpec(t *testing.T) sandbox.Spec {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.specs) == 0 {
		t.Fatal("没有调用 Create")
	}
	return f.specs[len(f.specs)-1]
}

func (f *fakeBackend) Check(ctx context.Context) error { return f.checkErr }

func (f *fakeBackend) Create(ctx context.Context, s sandbox.Spec) (string, error) {
	f.record("create")
	f.mu.Lock()
	f.specs = append(f.specs, s)
	hook := f.onCreate
	f.mu.Unlock()
	if hook != nil {
		hook(s)
	}
	if f.createErr != nil {
		return "", f.createErr
	}
	return "airlock-" + s.Name + "-box", nil
}

func (f *fakeBackend) Exec(ctx context.Context, id string, cmd []string) (sandbox.Session, error) {
	f.record("exec")
	f.mu.Lock()
	f.execs = append(f.execs, append([]string{id}, cmd...))
	f.mu.Unlock()
	return &fakeSession{}, nil
}

func (f *fakeBackend) AttachCommand(id string, cmd []string) []string {
	if f.noAttach {
		return nil
	}
	return append([]string{"docker", "exec", "-it", id}, cmd...)
}

func (f *fakeBackend) Logs(ctx context.Context, id string) (io.ReadCloser, error) {
	f.record("logs")
	return io.NopCloser(strings.NewReader(f.logs)), nil
}

func (f *fakeBackend) Remove(ctx context.Context, id string) error {
	f.record("remove")
	return f.removeErr
}

func (f *fakeBackend) Prune(ctx context.Context) error {
	f.record("prune")
	if f.onPrune != nil {
		f.onPrune()
	}
	return errors.New("fake: Docker 没有运行")
}

func (f *fakeBackend) RemoveCache(ctx context.Context, name string) error {
	f.record("remove-cache:" + name)
	return f.cacheErr
}

type fakeSession struct{}

func (*fakeSession) Read([]byte) (int, error)    { return 0, io.EOF }
func (*fakeSession) Write(b []byte) (int, error) { return len(b), nil }
func (*fakeSession) Close() error                { return nil }
func (*fakeSession) Resize(cols, rows int) error { return nil }

type fakeExplainer func(ctx context.Context, runID string, m HoldMeta) (Explanation, error)

func (f fakeExplainer) Explain(ctx context.Context, runID string, m HoldMeta) (Explanation, error) {
	return f(ctx, runID, m)
}

// fakeInspector 排除 .env，把 node_modules 当作依赖目录，把 Git 钩子标为高风险。
type fakeInspector struct{ entryErr error }

func (f fakeInspector) Entry(project string) ([]Finding, error) {
	if f.entryErr != nil {
		return nil, f.entryErr
	}
	return []Finding{
		{Path: ".env", Kind: Excluded, Reason: "含有密码"},
		{Path: "node_modules/", Kind: Cached, Reason: "依赖目录"},
		{Path: "AGENTS.md", Kind: Notice, Reason: "agent 会读取"},
	}, nil
}

func (fakeInspector) Exit(files string, changes []staging.Change) []Finding {
	var out []Finding
	for _, c := range changes {
		switch {
		case strings.HasPrefix(c.Path, ".git/hooks/"):
			out = append(out, Finding{Path: c.Path, Kind: HighRisk, Reason: "提交代码时会自动运行"})
		case strings.HasPrefix(c.Path, "node_modules/"):
			out = append(out, Finding{Path: c.Path, Kind: Cached, Reason: "依赖目录"})
		}
	}
	return out
}

type env struct {
	m       *Manager
	b       *fakeBackend
	dataDir string
	events  <-chan Event
}

func newEnv(t *testing.T, ex Explainer, in Inspector) *env {
	t.Helper()
	b := &fakeBackend{}
	dataDir := t.TempDir()
	m, err := New(Options{
		DataDir: dataDir, Backend: b, TermURL: "http://127.0.0.1:9999",
		Secret:    func(string) (string, error) { return fakeSecret, nil },
		Explainer: ex, Inspector: in,
	})
	if err != nil {
		t.Fatal(err)
	}
	events, cancel := m.Subscribe()
	t.Cleanup(cancel)
	t.Cleanup(func() {
		// 关闭测试中还在运行的闸门。
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, r := range m.runs {
			r.gate.Close()
			r.log.Close()
		}
	})
	return &env{m: m, b: b, dataDir: dataDir, events: events}
}

// newProject 建立一个小项目：源文件、.env、依赖目录和 Git 目录。
func newProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"main.go":                    "package main\n",
		".env":                       "TOKEN=fake-env-token\n",
		"node_modules/left/index.js": "module.exports = 1\n",
		".git/HEAD":                  "ref: refs/heads/main\n",
		"AGENTS.md":                  "# 项目说明\n",
	} {
		writeFile(t, filepath.Join(dir, name), content)
	}
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// create 新建并返回一个工作舱。
func (e *env) create(t *testing.T, preset string) Cabin {
	t.Helper()
	c, err := e.m.Create(newProject(t), preset, "standard")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// started 新建并启动一个工作舱。
func (e *env) started(t *testing.T, preset string) Cabin {
	t.Helper()
	c := e.create(t, preset)
	if err := e.m.Start(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	c, _ = e.m.get(c.ID)
	return c
}

func (e *env) status(t *testing.T, id string) Status {
	t.Helper()
	c, err := e.m.get(id)
	if err != nil {
		t.Fatal(err)
	}
	return c.Status
}

// next 读取事件，直到遇到 kind 类型的事件。
func (e *env) next(t *testing.T, kind string) Event {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-e.events:
			if ev.Kind == kind {
				return ev
			}
		case <-timeout:
			t.Fatalf("没有收到 %s 事件", kind)
		}
	}
}

// none 确认 d 时间内没有 kind 类型的事件。
func (e *env) none(t *testing.T, kind string, d time.Duration) {
	t.Helper()
	timeout := time.After(d)
	for {
		select {
		case ev := <-e.events:
			if ev.Kind == kind {
				t.Fatalf("不应该收到 %s 事件：%+v", kind, ev)
			}
		case <-timeout:
			return
		}
	}
}

// connect 像 agent 一样通过闸门发出 CONNECT 请求，返回闸门的响应状态码。
func connect(t *testing.T, port int, target string) <-chan int {
	t.Helper()
	out := make(chan int, 1)
	go func() {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			out <- -1
			return
		}
		defer conn.Close()
		fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			out <- -1
			return
		}
		out <- resp.StatusCode
	}()
	return out
}
