package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lijiahao818/airlock/internal/advisor"
	"github.com/lijiahao818/airlock/internal/audit"
	"github.com/lijiahao818/airlock/internal/cabin"
	"github.com/lijiahao818/airlock/internal/sandbox"
)

// 测试中使用的假错误和假密钥都用 fake- 前缀或 fake 开头。
var (
	errFakeDocker  = errors.New("fake: Docker 没有运行")
	errFakeHint    = errors.New("fake: 没有找到密钥")
	errFakeKey     = errors.New("fake: 这个密钥不对")
	errFakeAdvisor = errors.New("fake: 供应商不合法")
	errFakeRevert  = errors.New("fake: 工作舱没有在运行")
)

// fakeService 是 cabin.Service 的测试替身，记录每个方法的调用和参数。
// 返回值字段在测试开始前设置，handler 只读。
type fakeService struct {
	mu     sync.Mutex
	calls  []string // 每次调用一条记录，格式见各方法
	events chan cabin.Event
	cancel atomic.Int32 // Subscribe 的 cancel 被调用的次数

	checkDockerErr error

	list    []cabin.Cabin
	listErr error

	createCabin cabin.Cabin
	createErr   error

	agentURLValue string
	agentURLErr   error

	entryFindings []cabin.Finding
	entryErr      error

	reviewItems []cabin.ReviewItem
	reviewErr   error

	revertErr error
	applyErr  error

	reportValue audit.Report
	reportErr   error

	decideErr error

	resetDecisionErr error
}

func (f *fakeService) record(format string, args ...any) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	f.mu.Unlock()
}

// recorded 返回调用记录。record 内部格式化，外部只能读到字符串。
func (f *fakeService) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeService) CheckDocker(ctx context.Context) error {
	f.record("CheckDocker")
	return f.checkDockerErr
}

func (f *fakeService) List() ([]cabin.Cabin, error) {
	f.record("List")
	return f.list, f.listErr
}

func (f *fakeService) Create(project, preset, tier string) (cabin.Cabin, error) {
	f.record("Create %q %q %q", project, preset, tier)
	return f.createCabin, f.createErr
}

func (f *fakeService) Start(ctx context.Context, id string) error {
	f.record("Start %q", id)
	return nil
}

func (f *fakeService) Stop(ctx context.Context, id string) error {
	f.record("Stop %q", id)
	return nil
}

func (f *fakeService) Reset(ctx context.Context, id string) error {
	f.record("Reset %q", id)
	return nil
}

func (f *fakeService) AgentURL(id string) (string, error) {
	f.record("AgentURL %q", id)
	return f.agentURLValue, f.agentURLErr
}

func (f *fakeService) OpenTerminal(id string) error {
	f.record("OpenTerminal %q", id)
	return nil
}

func (f *fakeService) Attach(ctx context.Context, id, token string) (sandbox.Session, error) {
	f.record("Attach %q", id)
	return nil, nil
}

func (f *fakeService) EntryFindings(id string) ([]cabin.Finding, error) {
	f.record("EntryFindings %q", id)
	return f.entryFindings, f.entryErr
}

func (f *fakeService) ReviewItems(id string) ([]cabin.ReviewItem, error) {
	f.record("ReviewItems %q", id)
	return f.reviewItems, f.reviewErr
}

func (f *fakeService) Revert(id, path string) error {
	f.record("Revert %q %q", id, path)
	return f.revertErr
}

func (f *fakeService) Apply(id, path string) error {
	f.record("Apply %q %q", id, path)
	return f.applyErr
}

func (f *fakeService) FinishReview(id string) error {
	f.record("FinishReview %q", id)
	return nil
}

func (f *fakeService) Report(id string) (audit.Report, error) {
	f.record("Report %q", id)
	return f.reportValue, f.reportErr
}

func (f *fakeService) Decide(holdID string, allow bool) error {
	f.record("Decide %q %v", holdID, allow)
	return f.decideErr
}

func (f *fakeService) ResetDecision(id, host string) error {
	f.record("ResetDecision %q %q", id, host)
	return f.resetDecisionErr
}

func (f *fakeService) Subscribe() (<-chan cabin.Event, func()) {
	f.record("Subscribe")
	return f.events, func() { f.cancel.Add(1) }
}

// fakeKeys 是 console.Keys 的测试替身。Set 收到的密钥单独存放，不进入调用记录。
type fakeKeys struct {
	mu      sync.Mutex
	calls   []string
	hints   map[string]string
	secrets map[string]string // provider → 收到的密钥
	err     error             // Hint 之外的三个方法返回这个错误
	hintErr error
}

func (f *fakeKeys) record(format string, args ...any) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	f.mu.Unlock()
}

func (f *fakeKeys) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// secretOf 返回 Set 收到的密钥。
func (f *fakeKeys) secretOf(provider string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.secrets[provider]
}

func (f *fakeKeys) Hint(provider string) (string, error) {
	f.record("Hint %q", provider)
	return f.hints[provider], f.hintErr
}

func (f *fakeKeys) Set(provider, secret string) error {
	f.record("Set %q", provider)
	f.mu.Lock()
	f.secrets[provider] = secret
	f.mu.Unlock()
	return f.err
}

func (f *fakeKeys) Delete(provider string) error {
	f.record("Delete %q", provider)
	return f.err
}

func (f *fakeKeys) Test(ctx context.Context, provider string) error {
	f.record("Test %q", provider)
	return f.err
}

// fakeAdvisor 是 console.AdvisorSettings 的测试替身。
type fakeAdvisor struct {
	mu       sync.Mutex
	calls    []string
	settings advisor.Settings
	got      advisor.Settings // 最近一次 SetSettings 收到的设置
	err      error
}

func (f *fakeAdvisor) record(format string, args ...any) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	f.mu.Unlock()
}

func (f *fakeAdvisor) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeAdvisor) received() advisor.Settings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.got
}

func (f *fakeAdvisor) Settings() advisor.Settings {
	f.record("Settings")
	return f.settings
}

func (f *fakeAdvisor) SetSettings(s advisor.Settings) error {
	f.record("SetSettings %q %v %v", s.Provider, s.Enabled, s.NoticeShown)
	f.mu.Lock()
	f.got = s
	f.mu.Unlock()
	return f.err
}

const fakeToken = "fake-token-1234567890"

// env 把测试替身和真实 handler 装在一起。
type env struct {
	svc  *fakeService
	keys *fakeKeys
	adv  *fakeAdvisor
	srv  *httptest.Server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		svc:  &fakeService{events: make(chan cabin.Event, 16)},
		keys: &fakeKeys{hints: map[string]string{}, secrets: map[string]string{}},
		adv:  &fakeAdvisor{},
	}
	h := New(Deps{
		Cabins:     e.svc,
		Keys:       e.keys,
		Advisor:    e.adv,
		PickFolder: func(ctx context.Context) (string, error) { return `D:\fake-project`, nil },
	}, fakeToken)
	e.srv = httptest.NewServer(h)
	t.Cleanup(e.srv.Close)
	return e
}

// request 发送一个请求并返回响应。token、host 和 origin 为空串时不做对应设置，
// 所以空 token 表示不带令牌。
func (e *env) request(t *testing.T, method, target, body, token, host, origin string) *http.Response {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.srv.URL+target, rd)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("X-Airlock-Token", token)
	}
	if host != "" {
		req.Host = host
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// body 读出并关闭响应体，返回文本。
func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
