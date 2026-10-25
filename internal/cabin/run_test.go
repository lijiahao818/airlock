package cabin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lijiahao818/airlock/internal/gate"
	"github.com/lijiahao818/airlock/internal/sandbox"
	"github.com/lijiahao818/airlock/presets"
)

// fakeWebToken 的格式与 dsh 0.2.1-rc.2 的实测相同：43 个 base64url 字符。
const fakeWebToken = "fake_abcdefghijklmnopqrstuvwxyz0123456789-_"

const dshLogs = "dsh web: listening on 172.22.0.2 over plain HTTP; restrict this port to a trusted proxy or network\n" +
	"dsh web: http://172.22.0.2:3080/?token=" + fakeWebToken + "\n"

func listening(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err == nil {
		c.Close()
	}
	return err == nil
}

// AC-4、AC-5：按第 6.1 节的顺序启动。排除的文件不进暂存区，Spec 中只有占位符和地址。
func TestStartTerminal(t *testing.T) {
	e := newEnv(t, nil, fakeInspector{})
	c := e.create(t, "claude-code")
	var atCreate struct{ copied, excluded, gateUp, logOpen bool }
	e.b.onCreate = func(s sandbox.Spec) {
		_, err := os.Stat(filepath.Join(s.StagingDir, "main.go"))
		atCreate.copied = err == nil
		_, err1 := os.Stat(filepath.Join(s.StagingDir, ".env"))
		_, err2 := os.Stat(filepath.Join(s.StagingDir, "node_modules"))
		atCreate.excluded = os.IsNotExist(err1) && os.IsNotExist(err2)
		atCreate.gateUp = listening(s.GatePort)
		runs, _ := os.ReadDir(filepath.Join(e.dataDir, "runs"))
		atCreate.logOpen = len(runs) == 1
	}
	if err := e.m.Start(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	if pg := e.next(t, "progress").Progress; pg.Done != pg.Total || pg.Total != 3 {
		t.Fatalf("progress = %+v", pg)
	}
	if st := e.next(t, "status"); st.Status != Running || st.Cabin != c.ID {
		t.Fatalf("status = %+v", st)
	}
	if atCreate != (struct{ copied, excluded, gateUp, logOpen bool }{true, true, true, true}) {
		t.Fatalf("调用 Create 时：%+v", atCreate)
	}

	s := e.b.lastSpec(t)
	if s.Name != c.ID || !strings.HasPrefix(s.Image, "airlock-claude-code:") || len(s.Dockerfile) == 0 || len(s.Cmd) != 0 || s.WebPort != 0 {
		t.Fatalf("Spec = %+v", s)
	}
	if want := []string{".venv", "node_modules"}; !reflect.DeepEqual(s.Volumes, want) {
		t.Fatalf("Volumes = %q, want %q", s.Volumes, want)
	}
	if s.Env["ANTHROPIC_BASE_URL"] != "http://airlock-gate:8080/anthropic" || s.Env["HTTPS_PROXY"] != "http://airlock-gate:8080" {
		t.Fatalf("Env = %q", s.Env)
	}
	if p := s.Env["ANTHROPIC_API_KEY"]; p == "" || p == fakeSecret || strings.Contains(p, "{") {
		t.Fatalf("ANTHROPIC_API_KEY 应该是闸门签发的占位符：%q", p)
	}
	// Claude Code 预先批准这个占位符，不询问用户。
	var cfg struct {
		CustomAPIKeyResponses struct{ Approved []string } `json:"customApiKeyResponses"`
	}
	key := s.Env["ANTHROPIC_API_KEY"]
	if err := json.Unmarshal([]byte(s.Files["~/.claude.json"]), &cfg); err != nil || !reflect.DeepEqual(cfg.CustomAPIKeyResponses.Approved, []string{key[len(key)-20:]}) {
		t.Fatalf("~/.claude.json = %q，%v", s.Files["~/.claude.json"], err)
	}
	// 红线 1：真实密钥不进舱。
	if b, _ := json.Marshal(s); strings.Contains(string(b), fakeSecret) {
		t.Fatal("Spec 中出现了真实密钥")
	}

	got, _ := e.m.get(c.ID)
	if got.Status != Running || got.RunID == "" {
		t.Fatalf("cabin = %+v", got)
	}
	raw, _ := e.m.AgentURL(c.ID)
	u, err := url.Parse(raw)
	if err != nil || u.Host != "127.0.0.1:9999" || u.Query().Get("cabin") != c.ID || !strings.HasPrefix(u.Fragment, "token=") || len(u.Fragment) < 40 {
		t.Fatalf("AgentURL = %q", raw)
	}
	entry, _ := e.m.EntryFindings(c.ID)
	if len(entry) != 3 || entry[0].Path != ".env" {
		t.Fatalf("EntryFindings = %+v", entry)
	}
}

// 闸门把暂扣交给 cabin，用户的拒绝写入审计记录和运行报告。
func TestStartConnectsGateToHolds(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.started(t, "codex")
	status := connect(t, e.b.lastSpec(t).GatePort, "evil.example:443")
	h := e.next(t, "hold").Hold
	if h.Agent != "Codex" || h.Host != "evil.example" || h.Cabin != c.ID || h.RunID != c.RunID {
		t.Fatalf("hold = %+v", h)
	}
	if err := e.m.Decide(h.ID, false); err != nil {
		t.Fatal(err)
	}
	if code := <-status; code != 403 {
		t.Fatalf("拒绝后闸门返回 %d", code)
	}
	if r := e.next(t, "request").Request; r.Rule != "user-deny" || r.Allowed {
		t.Fatalf("request = %+v", r)
	}
	rep, err := e.m.Report(c.ID)
	if err != nil || len(rep.Holds) != 1 || rep.Holds[0].Rule != "user-deny" {
		t.Fatalf("Report = %+v, %v", rep, err)
	}
	// 撤销决定后，同一个主机再次暂扣。
	if err := e.m.ResetDecision(c.ID, "evil.example"); err != nil {
		t.Fatal(err)
	}
	connect(t, e.b.lastSpec(t).GatePort, "evil.example:443")
	h2 := e.next(t, "hold").Hold
	e.m.Decide(h2.ID, false)
}

// AC-12：web 型预设从启动输出中提取令牌。网址的主机和端口由 cabin 拼接。
func TestStartWeb(t *testing.T) {
	e := newEnv(t, nil, nil)
	e.b.logs = dshLogs
	c := e.started(t, "deepseek-harness")
	s := e.b.lastSpec(t)
	if s.WebPort != 3080 || s.HostPort == 0 || len(s.Cmd) == 0 {
		t.Fatalf("Spec = %+v", s)
	}
	if cmd := strings.Join(s.Cmd, " "); !strings.Contains(cmd, fmt.Sprintf("--trusted-host 127.0.0.1:%d", s.HostPort)) || strings.Contains(cmd, "{") {
		t.Fatalf("Cmd = %q", s.Cmd)
	}
	if s.Env["DEEPSEEK_BASE_URL"] != "http://airlock-gate:8080/deepseek" {
		t.Fatalf("Env = %q", s.Env)
	}
	raw, _ := e.m.AgentURL(c.ID)
	if want := fmt.Sprintf("http://127.0.0.1:%d/?token=%s", s.HostPort, fakeWebToken); raw != want {
		t.Fatalf("AgentURL = %q, want %q", raw, want)
	}
	// 网页型 agent 没有终端。
	if _, err := e.m.Attach(context.Background(), c.ID, "x"); err == nil {
		t.Fatal("网页型 agent 不能 Attach")
	}
	if err := e.m.OpenTerminal(c.ID); err == nil {
		t.Fatal("网页型 agent 不能打开终端窗口")
	}
}

// AC-12：没有找到令牌时，Start 失败，清理已创建的资源，状态仍为 Idle。
func TestStartWebWithoutTokenCleansUp(t *testing.T) {
	for name, logs := range map[string]string{
		"没有令牌": "dsh web: listening on 172.22.0.2\n",
		"令牌太短": "dsh web: http://172.22.0.2:3080/?token=short\n",
		"前缀不对": "evil: http://172.22.0.2:3080/?token=" + fakeWebToken + "\n",
		"空输出":  "",
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, nil, nil)
			e.b.logs = logs
			c := e.create(t, "deepseek-harness")
			if err := e.m.Start(context.Background(), c.ID); err == nil {
				t.Fatal("Start 应该失败")
			}
			e.assertCleanedUp(t, c.ID, 1)
		})
	}
}

// assertCleanedUp 检查启动失败后：沙箱已删除，闸门已关闭，暂存区已删除，状态仍为 Idle。
func (e *env) assertCleanedUp(t *testing.T, id string, removes int) {
	t.Helper()
	if e.status(t, id) != Idle {
		t.Fatalf("状态 = %q", e.status(t, id))
	}
	if entry, _ := e.m.EntryFindings(id); len(entry) != 0 {
		t.Fatalf("启动失败后不能保留这次的进舱检查结果：%+v", entry)
	}
	if e.b.called("remove") != removes {
		t.Fatalf("沙箱没有删除：%q", e.b.calls)
	}
	if e.b.called("create") > 0 && listening(e.b.lastSpec(t).GatePort) {
		t.Fatal("闸门没有关闭")
	}
	if _, err := os.Stat(e.m.areaDir(id)); !os.IsNotExist(err) {
		t.Fatalf("暂存区没有删除：%v", err)
	}
	if _, err := e.m.AgentURL(id); err == nil {
		t.Fatal("启动失败后不能有 AgentURL")
	}
}

func TestStartFailures(t *testing.T) {
	t.Run("Create 失败", func(t *testing.T) {
		e := newEnv(t, nil, fakeInspector{})
		e.b.createErr = errors.New("fake: 镜像构建失败")
		c := e.create(t, "claude-code")
		if err := e.m.Start(context.Background(), c.ID); err == nil {
			t.Fatal("Start 应该失败")
		}
		// sandbox.Create 失败时自己清理，cabin 不再调用 Remove。
		e.assertCleanedUp(t, c.ID, 0)
	})
	t.Run("进舱检查失败", func(t *testing.T) {
		e := newEnv(t, nil, fakeInspector{entryErr: errors.New("fake: 读取失败")})
		c := e.create(t, "claude-code")
		if err := e.m.Start(context.Background(), c.ID); err == nil {
			t.Fatal("Start 应该失败")
		}
		if e.b.called("create") != 0 {
			t.Fatal("进舱检查失败时不能创建沙箱")
		}
		e.assertCleanedUp(t, c.ID, 0)
	})
	t.Run("项目文件夹被删除", func(t *testing.T) {
		e := newEnv(t, nil, nil)
		c := e.create(t, "claude-code")
		os.RemoveAll(c.Project)
		if err := e.m.Start(context.Background(), c.ID); err == nil {
			t.Fatal("Start 应该失败")
		}
		e.assertCleanedUp(t, c.ID, 0)
	})
}

// AC-1：Running 状态不能再次 Start。正在启动时，其他操作返回错误。
func TestStartTransitions(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	if err := e.m.Start(context.Background(), c.ID); err == nil {
		t.Fatal("Running 状态不能 Start")
	}
	if e.status(t, c.ID) != Running || e.b.called("create") != 1 {
		t.Fatal("拒绝后状态和资源不能改变")
	}

	e2 := newEnv(t, nil, nil)
	c2 := e2.create(t, "claude-code")
	entered, release := make(chan struct{}), make(chan struct{})
	e2.b.onCreate = func(sandbox.Spec) { close(entered); <-release }
	done := make(chan error)
	go func() { done <- e2.m.Start(context.Background(), c2.ID) }()
	<-entered
	if err := e2.m.Start(context.Background(), c2.ID); err == nil || !strings.Contains(err.Error(), "正在处理") {
		t.Fatalf("启动中再次 Start = %v", err)
	}
	if err := e2.m.Reset(context.Background(), c2.ID); err == nil {
		t.Fatal("启动中不能 Reset")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// AC-11：Attach 用终端令牌连接 agent 会话。令牌错误或运行已结束时，不调用 Exec。
func TestAttach(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	raw, _ := e.m.AgentURL(c.ID)
	u, _ := url.Parse(raw)
	token := strings.TrimPrefix(u.Fragment, "token=")
	ctx := context.Background()
	for _, bad := range []string{"", "fake-wrong-token", token[:len(token)-1], token + "x"} {
		if _, err := e.m.Attach(ctx, c.ID, bad); err == nil {
			t.Fatalf("Attach 接受了错误的令牌 %q", bad)
		}
	}
	if e.b.called("exec") != 0 {
		t.Fatal("令牌错误时不能调用 Exec")
	}
	s, err := e.m.Attach(ctx, c.ID, token)
	if err != nil || s == nil {
		t.Fatal(err)
	}
	if want := []string{"airlock-" + c.ID + "-box", "claude"}; !reflect.DeepEqual(e.b.execs[0], want) {
		t.Fatalf("Exec = %q, want %q", e.b.execs[0], want)
	}
	// 停止后令牌失效。
	if err := e.m.Stop(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Attach(ctx, c.ID, token); err == nil {
		t.Fatal("运行结束后 Attach 应该返回错误")
	}
	if e.b.called("exec") != 1 {
		t.Fatal("运行结束后不能调用 Exec")
	}
	// 下一次运行使用新的令牌。
	if err := e.m.FinishReview(c.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Start(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Attach(ctx, c.ID, token); err == nil {
		t.Fatal("上一次运行的令牌不能使用")
	}
}

// {placeholder-tail} 展开为占位符的最后 20 个字符，与 {placeholder} 互不影响。
func TestSpecExpandsPlaceholderTail(t *testing.T) {
	g, err := gate.Start(gate.Config{
		Providers: []string{"anthropic"},
		Secret:    func(string) (string, error) { return "", errors.New("fake: 测试中没有密钥") },
		Ask:       func(context.Context, gate.Event) bool { return false },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	ph := g.Placeholder("anthropic")
	p := presets.Preset{
		Provider: "anthropic",
		Env:      map[string]string{"KEY": "{placeholder}", "TAIL": "{placeholder-tail}"},
		Files:    map[string]string{"~/f": `["{placeholder-tail}"]`},
	}
	s := newEnv(t, nil, nil).m.spec(Cabin{ID: "c1"}, p, &run{gate: g}, "", 0, nil)
	if len(ph) <= 20 || s.Env["KEY"] != ph || s.Env["TAIL"] != ph[len(ph)-20:] || s.Files["~/f"] != `["`+ph[len(ph)-20:]+`"]` {
		t.Fatalf("占位符 %q，Env %q，Files %q", ph, s.Env, s.Files)
	}
}

func TestOpenTerminal(t *testing.T) {
	var got []string
	old := openTerminal
	openTerminal = func(argv []string) error { got = argv; return nil }
	t.Cleanup(func() { openTerminal = old })

	e := newEnv(t, nil, nil)
	c := e.create(t, "codex")
	if err := e.m.OpenTerminal(c.ID); err == nil || got != nil {
		t.Fatal("没有运行时不能打开终端窗口")
	}
	e.m.Start(context.Background(), c.ID)
	if err := e.m.OpenTerminal(c.ID); err != nil {
		t.Fatal(err)
	}
	if want := []string{"docker", "exec", "-it", "airlock-" + c.ID + "-box", "codex", "--no-daemon"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

// Stop 删除沙箱失败时，状态不变，可以再次停止。停止时结束正在进行的暂扣。
func TestStop(t *testing.T) {
	e := newEnv(t, nil, nil)
	c := e.started(t, "claude-code")
	port := e.b.lastSpec(t).GatePort
	status := connect(t, port, "evil.example:443")
	e.next(t, "hold")

	e.b.removeErr = errors.New("fake: Docker 没有响应")
	if err := e.m.Stop(context.Background(), c.ID); err == nil {
		t.Fatal("Stop 应该失败")
	}
	if e.status(t, c.ID) != Running || !listening(port) {
		t.Fatal("Stop 失败时状态和闸门不能改变")
	}
	e.b.removeErr = nil
	if err := e.m.Stop(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	e.next(t, "hold-closed")
	if code := <-status; code == 200 {
		t.Fatal("停止时暂扣的请求不能被放行")
	}
	if e.status(t, c.ID) != Review || listening(port) {
		t.Fatal("停止后应该进入审查，闸门应该关闭")
	}
	if err := e.m.Stop(context.Background(), c.ID); err == nil {
		t.Fatal("Review 状态不能 Stop")
	}
	if _, err := e.m.AgentURL(c.ID); err == nil {
		t.Fatal("停止后不能有 AgentURL")
	}
}

func TestWebTokenRejectsBadFormat(t *testing.T) {
	e := newEnv(t, nil, nil)
	for _, logs := range []string{
		"token=<script>alert(1)</script>xxxxxxxxxxxx\n",
		"token=" + strings.Repeat("a", 300) + "\n",
		"token=fake token with spaces\n",
	} {
		e.b.logs = logs
		if _, err := e.m.webToken(context.Background(), "box", `token=(.+)$`); err == nil {
			t.Errorf("应该拒绝 %q", logs)
		}
	}
	e.b.logs = strings.Repeat("noise\n", maxLogLines) + "token=" + fakeWebToken + "\n"
	if _, err := e.m.webToken(context.Background(), "box", `token=(.+)$`); err == nil {
		t.Error("超过行数上限后不应该继续读取")
	}
	e.b.logs = "token=" + fakeWebToken + "\r\n"
	if tok, err := e.m.webToken(context.Background(), "box", `^token=([A-Za-z0-9_-]+)$`); err != nil || tok != fakeWebToken {
		t.Errorf("行尾的 \\r\\n 应该被去掉：%q, %v", tok, err)
	}
}

// TestEntryPlan 检查进舱检查结果到复制计划的映射：Excluded 的文件不复制，
// Cached 的目录不复制并挂缓存卷，Notice 的文件照常复制（Issue #74 AC-1）。
func TestEntryPlan(t *testing.T) {
	skip, volumes := entryPlan([]Finding{
		{Path: ".npmrc", Kind: Excluded},
		{Path: ".aws/credentials", Kind: Excluded},
		{Path: "node_modules/", Kind: Cached},
		{Path: "AGENTS.md", Kind: Notice},
	})
	for _, p := range []string{".npmrc", ".aws/credentials", "node_modules"} {
		if !skip(p) {
			t.Errorf("skip(%q) = false，want true", p)
		}
	}
	for _, p := range []string{"AGENTS.md", "src/main.go"} {
		if skip(p) {
			t.Errorf("skip(%q) = true，want false", p)
		}
	}
	if !slices.Contains(volumes, "node_modules") {
		t.Errorf("volumes = %v，want 含 node_modules", volumes)
	}
}
