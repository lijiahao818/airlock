package webterm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/lijiahao818/airlock/internal/sandbox"
)

type Attacher interface {
	Attach(ctx context.Context, id, token string) (sandbox.Session, error)
}

const (
	protocol      = "airlock"        // 握手成功时服务端选择的子协议
	maxMessage    = 64 << 10         // 一条消息的上限，见 docs/architecture.md 第 5.11 节
	maxSize       = 1000             // 行数和列数的上限
	attachTimeout = 30 * time.Second // 连接 agent 会话的时间上限
)

// New 返回网页终端的 HTTP 处理器。origin 是网页终端自己的源，例如 http://127.0.0.1:<端口>。
func New(a Attacher, origin string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /ws", &wsHandler{a: a, origin: origin})
	return mux
}

type wsHandler struct {
	a      Attacher
	origin string
}

// ServeHTTP 先检查 Origin、升级请求、子协议和工作舱 ID，再用令牌调用 Attach。任一检查不通过时拒绝握手。
func (h *wsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 浏览器在 WebSocket 握手中总会带上 Origin。coder/websocket 在没有 Origin 时不拒绝，所以这里自己检查。
	if r.Header.Get("Origin") != h.origin {
		http.Error(w, "webterm: 只接受网页终端页面的连接。", http.StatusForbidden)
		return
	}
	// 不是升级请求时不调用 Attach，普通的 HTTP 请求不能在沙箱中启动会话。
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "webterm: 这个地址只接受 WebSocket 连接。", http.StatusBadRequest)
		return
	}
	token, ok := tokenOf(r.Header)
	id := r.URL.Query().Get("cabin")
	if !ok || id == "" {
		http.Error(w, "webterm: 缺少工作舱或令牌。请从控制台重新打开运行页面。", http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), attachTimeout)
	s, err := h.a.Attach(ctx, id, token)
	cancel()
	if err != nil {
		http.Error(w, "webterm: 无法连接 agent。请从控制台重新打开运行页面。", http.StatusForbidden)
		return
	}
	defer s.Close()
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{protocol}})
	if err != nil {
		return // Accept 已经写入了错误响应
	}
	defer c.CloseNow()
	c.SetReadLimit(maxMessage)
	relay(r.Context(), c, s)
}

// tokenOf 从 Sec-WebSocket-Protocol 中取出令牌。子协议必须正好是 airlock 和一个令牌。
func tokenOf(h http.Header) (string, bool) {
	var token string
	seen := false
	n := 0
	for _, v := range h.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			n++
			if p == protocol {
				seen = true
			} else {
				token = p
			}
		}
	}
	return token, seen && n == 2 && token != ""
}

// relay 在 WebSocket 和终端会话之间转发数据，直到任一方结束。
func relay(ctx context.Context, c *websocket.Conn, s sandbox.Session) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// 终端输出来自沙箱，原样作为二进制消息发给页面，由 xterm 显示为文字。
	go func() {
		defer cancel()
		buf := make([]byte, 32<<10)
		for {
			n, err := s.Read(buf)
			if n > 0 && c.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
				return
			}
			if err != nil {
				c.Close(websocket.StatusNormalClosure, "agent 会话已结束")
				return
			}
		}
	}()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return // 页面断开、消息超过上限或输出已经结束
		}
		if typ != websocket.MessageText {
			c.Close(websocket.StatusUnsupportedData, "只接受文本消息")
			return
		}
		if err := handle(s, data); err != nil {
			c.Close(websocket.StatusPolicyViolation, "消息格式不对")
			return
		}
	}
}

// message 是页面发来的文本消息：键盘输入或窗口大小。
type message struct {
	Input *string `json:"input"`
	Cols  int     `json:"cols"`
	Rows  int     `json:"rows"`
}

var errBadMessage = errors.New("webterm: 消息格式不对")

func handle(s sandbox.Session, data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m message
	if err := dec.Decode(&m); err != nil || dec.More() {
		return errBadMessage
	}
	switch {
	case m.Input != nil && m.Cols == 0 && m.Rows == 0:
		_, err := io.WriteString(s, *m.Input)
		return err
	case m.Input == nil && m.Cols >= 1 && m.Cols <= maxSize && m.Rows >= 1 && m.Rows <= maxSize:
		return s.Resize(m.Cols, m.Rows)
	}
	return errBadMessage
}
