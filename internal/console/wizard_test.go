package console

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lijiahao818/airlock/internal/cabin"
)

func wizardDone(t *testing.T, e *env) string {
	t.Helper()
	return strings.TrimSpace(body(t, e.request(t, http.MethodGet, "/api/wizard", "", fakeToken, "", "")))
}

// 全新的数据目录中显示向导。完成后记在数据目录里，控制台换了端口也不再显示（#86）。
func TestWizard(t *testing.T) {
	e := newEnv(t)
	if got := wizardDone(t, e); got != `{"done":false}` {
		t.Fatalf("第一次启动 = %s", got)
	}
	resp := e.request(t, http.MethodPost, "/api/wizard", "", fakeToken, "", "")
	body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("完成向导：状态码 %d", resp.StatusCode)
	}
	if got := wizardDone(t, e); got != `{"done":true}` {
		t.Fatalf("完成后 = %s", got)
	}
	// 重启后控制台是新的实例，用同一个数据目录。
	e2 := &env{svc: &fakeService{}, keys: e.keys, adv: e.adv, dir: e.dir}
	e2.srv = httptest.NewServer(New(Deps{Cabins: e2.svc, Keys: e2.keys, Advisor: e2.adv, DataDir: e.dir}, fakeToken))
	t.Cleanup(e2.srv.Close)
	if got := wizardDone(t, e2); got != `{"done":true}` {
		t.Fatalf("重启后 = %s", got)
	}
}

// 已经有工作舱的用户不再看到向导，例如升级前就在使用 Airlock 的用户。
func TestWizardDoneWithCabins(t *testing.T) {
	e := newEnv(t)
	e.svc.list = []cabin.Cabin{{ID: "c1", Name: "fake-project"}}
	if got := wizardDone(t, e); got != `{"done":true}` {
		t.Fatalf("有工作舱时 = %s", got)
	}
}

// 记录失败时返回中文错误，说明后果。
func TestWizardSaveFails(t *testing.T) {
	e := newEnv(t)
	// 让数据目录变成一个文件，写入必然失败。
	os.RemoveAll(e.dir)
	if err := os.WriteFile(e.dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	resp := e.request(t, http.MethodPost, "/api/wizard", "", fakeToken, "", "")
	got := body(t, resp)
	if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(got, "下次打开 Airlock 时会再次显示向导") {
		t.Fatalf("状态码 %d，响应 %s", resp.StatusCode, got)
	}
}

func TestWizardNeedsToken(t *testing.T) {
	e := newEnv(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		resp := e.request(t, method, "/api/wizard", "", "", "", "")
		body(t, resp)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s 没有令牌：状态码 %d", method, resp.StatusCode)
		}
	}
	if _, err := os.Stat(filepath.Join(e.dir, wizardFile)); err == nil {
		t.Fatal("没有令牌时写入了向导状态")
	}
}
