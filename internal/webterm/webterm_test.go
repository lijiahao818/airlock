package webterm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/lijiahao818/airlock/internal/sandbox"
)

const fakeToken = "fake-term-token"

// fakeSession 记录键盘输入和窗口大小。测试向 out 写入的内容就是终端输出。
type fakeSession struct {
	r      *io.PipeReader
	out    *io.PipeWriter
	input  chan string
	sizes  chan [2]int
	closed chan struct{}
	once   sync.Once
}

func newFakeSession() *fakeSession {
	r, w := io.Pipe()
	return &fakeSession{r: r, out: w, input: make(chan string, 10), sizes: make(chan [2]int, 10), closed: make(chan struct{})}
}

func (s *fakeSession) Read(p []byte) (int, error)  { return s.r.Read(p) }
func (s *fakeSession) Write(p []byte) (int, error) { s.input <- string(p); return len(p), nil }
func (s *fakeSession) Resize(cols, rows int) error { s.sizes <- [2]int{cols, rows}; return nil }
func (s *fakeSession) Close() error {
	s.once.Do(func() { close(s.closed); s.r.Close() })
	return nil
}

// fakeAttacher 只接受 fakeToken，记录调用次数。
type fakeAttacher struct {
	mu      sync.Mutex
	calls   int
	id      string
	session *fakeSession
}

func (f *fakeAttacher) Attach(ctx context.Context, id, token string) (sandbox.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.id = id
	if token != fakeToken {
		return nil, errors.New("fake: 令牌无效")
	}
	f.session = newFakeSession()
	return f.session, nil
}

func (f *fakeAttacher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// server 启动网页终端，返回它的源。
func server(t *testing.T, a Attacher) string {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	srv.Start()
	srv.Config.Handler = New(a, srv.URL)
	t.Cleanup(srv.Close)
	return srv.URL
}

func dial(t *testing.T, origin, path, originHeader string, protocols ...string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{}
	if originHeader != "" {
		h.Set("Origin", originHeader)
	}
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(origin, "http")+path, &websocket.DialOptions{HTTPHeader: h, Subprotocols: protocols})
}

// AC-2：Origin 或子协议不对时拒绝握手，不调用 Attach。
func TestHandshakeRejects(t *testing.T) {
	a := &fakeAttacher{}
	origin := server(t, a)
	cases := []struct {
		name, path, origin string
		protocols          []string
	}{
		{"没有 Origin", "/ws?cabin=c1", "", []string{"airlock", fakeToken}},
		{"其他网站", "/ws?cabin=c1", "http://evil.example", []string{"airlock", fakeToken}},
		{"同一主机的其他端口", "/ws?cabin=c1", "http://127.0.0.1:1", []string{"airlock", fakeToken}},
		{"localhost 不等于 127.0.0.1", "/ws?cabin=c1", strings.Replace(origin, "127.0.0.1", "localhost", 1), []string{"airlock", fakeToken}},
		{"没有子协议", "/ws?cabin=c1", origin, nil},
		{"只有 airlock", "/ws?cabin=c1", origin, []string{"airlock"}},
		{"只有令牌", "/ws?cabin=c1", origin, []string{fakeToken}},
		{"两个令牌", "/ws?cabin=c1", origin, []string{"airlock", fakeToken, "fake-other"}},
		{"大小写不同的 airlock", "/ws?cabin=c1", origin, []string{"Airlock", fakeToken}},
		{"没有工作舱", "/ws", origin, []string{"airlock", fakeToken}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn, resp, err := dial(t, origin, c.path, c.origin, c.protocols...)
			if err == nil {
				conn.CloseNow()
				t.Fatal("握手应该失败")
			}
			if resp == nil || resp.StatusCode != http.StatusForbidden {
				t.Fatalf("响应 = %+v", resp)
			}
		})
	}
	if a.count() != 0 {
		t.Fatalf("拒绝握手时调用了 %d 次 Attach", a.count())
	}
}

// 普通的 HTTP 请求即使带着正确的令牌，也不调用 Attach。
func TestPlainRequestDoesNotAttach(t *testing.T) {
	a := &fakeAttacher{}
	origin := server(t, a)
	req, _ := http.NewRequest("GET", origin+"/ws?cabin=c1", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Sec-WebSocket-Protocol", "airlock, "+fakeToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || a.count() != 0 {
		t.Fatalf("状态码 %d，Attach 调用 %d 次", resp.StatusCode, a.count())
	}
	// / 提供网页终端页面，见 page_test.go。
	for _, path := range []string{"/ws/x", "/other"} {
		resp, err := http.Get(origin + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d", path, resp.StatusCode)
		}
	}
}

// Attach 成功但握手不完整时，关闭刚连接的会话，不留下没人使用的连接。
func TestIncompleteHandshakeClosesSession(t *testing.T) {
	a := &fakeAttacher{}
	origin := server(t, a)
	req, _ := http.NewRequest("GET", origin+"/ws?cabin=c1", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Protocol", "airlock, "+fakeToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 400 || a.count() != 1 {
		t.Fatalf("状态码 %d，Attach 调用 %d 次", resp.StatusCode, a.count())
	}
	waitClosed(t, a.session)
}

// AC-3：Attach 返回错误时拒绝握手。
func TestAttachErrorRejects(t *testing.T) {
	a := &fakeAttacher{}
	origin := server(t, a)
	_, resp, err := dial(t, origin, "/ws?cabin=c1", origin, "airlock", "fake-wrong-token")
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden || a.count() != 1 {
		t.Fatalf("err = %v，resp = %+v，Attach 调用 %d 次", err, resp, a.count())
	}
}

// open 建立一个成功的连接。
func open(t *testing.T) (*websocket.Conn, *fakeAttacher) {
	t.Helper()
	a := &fakeAttacher{}
	origin := server(t, a)
	c, _, err := dial(t, origin, "/ws?cabin=c1", origin, "airlock", fakeToken)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c, a
}

func waitClosed(t *testing.T, s *fakeSession) {
	t.Helper()
	select {
	case <-s.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("会话没有关闭")
	}
}

// AC-3、AC-4、AC-5：握手成功后选择 airlock，转发输入、窗口大小和输出。页面断开时关闭会话。
func TestRelay(t *testing.T) {
	c, a := open(t)
	if c.Subprotocol() != "airlock" || a.id != "c1" {
		t.Fatalf("子协议 = %q，工作舱 = %q", c.Subprotocol(), a.id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s := a.session

	c.Write(ctx, websocket.MessageText, []byte(`{"input":"ls -la\r"}`))
	if got := <-s.input; got != "ls -la\r" {
		t.Fatalf("输入 = %q", got)
	}
	c.Write(ctx, websocket.MessageText, []byte(`{"cols":120,"rows":40}`))
	if got := <-s.sizes; got != [2]int{120, 40} {
		t.Fatalf("窗口大小 = %v", got)
	}
	go s.out.Write([]byte("\x1b[31mhello\x1b[0m"))
	typ, data, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || string(data) != "\x1b[31mhello\x1b[0m" {
		t.Fatalf("输出 = %v %q %v", typ, data, err)
	}
	c.Close(websocket.StatusNormalClosure, "")
	waitClosed(t, s)
}

// AC-4：消息超过上限、不是文本或格式不对时关闭连接，并关闭会话。
func TestClosesOnBadMessages(t *testing.T) {
	cases := map[string]struct {
		typ  websocket.MessageType
		data string
	}{
		"二进制消息":     {websocket.MessageBinary, `{"input":"x"}`},
		"不是 JSON":   {websocket.MessageText, `ls`},
		"未知字段":      {websocket.MessageText, `{"input":"x","cmd":"rm"}`},
		"同时有输入和大小":  {websocket.MessageText, `{"input":"x","cols":80,"rows":24}`},
		"只有列数":      {websocket.MessageText, `{"cols":80}`},
		"列数为 0":     {websocket.MessageText, `{"cols":0,"rows":24}`},
		"行数超过上限":    {websocket.MessageText, `{"cols":80,"rows":1001}`},
		"两个对象":      {websocket.MessageText, `{"input":"a"}{"input":"b"}`},
		"空对象":       {websocket.MessageText, `{}`},
		"超过 64 KiB": {websocket.MessageText, `{"input":"` + strings.Repeat("a", maxMessage) + `"}`},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			c, a := open(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c.Write(ctx, m.typ, []byte(m.data))
			if _, _, err := c.Read(ctx); err == nil {
				t.Fatal("连接应该被关闭")
			}
			waitClosed(t, a.session)
			if len(a.session.input) != 0 || len(a.session.sizes) != 0 {
				t.Fatal("格式不对的消息不能转发给会话")
			}
		})
	}
}

// 一条最大长度以内的消息可以正常转发。
func TestLargestMessage(t *testing.T) {
	c, a := open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	text := strings.Repeat("a", maxMessage-len(`{"input":""}`))
	c.Write(ctx, websocket.MessageText, []byte(`{"input":"`+text+`"}`))
	if got := <-a.session.input; got != text {
		t.Fatalf("输入长度 = %d", len(got))
	}
}

// agent 会话结束时，网页终端关闭连接。
func TestSessionEndClosesConnection(t *testing.T) {
	c, a := open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.session.out.Close()
	_, _, err := c.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("Read = %v", err)
	}
	waitClosed(t, a.session)
}
