package gate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Event 是一次出网判定的元数据。不含请求体、请求头和查询参数。
type Event struct {
	Time     time.Time `json:"time"`
	Host     string    `json:"host"`
	Port     int       `json:"port"`
	Method   string    `json:"method"`         // CONNECT，或模型路由上的 HTTP 方法
	Path     string    `json:"path,omitempty"` // 只有模型路由有路径，不含查询参数
	Rule     string    `json:"rule"`           // 命中的规则，取值见 docs/architecture.md 第 5.1 节
	Allowed  bool      `json:"allowed"`
	Provider string    `json:"provider,omitempty"` // 换上了真实密钥时为供应商名，否则为空
}

type Config struct {
	// Host 是非 CONNECT 请求必须携带的 Host 头。为空时要求 127.0.0.1:<闸门端口>。
	Host      string
	Allow     []string // 自动放行的主机，来自安全档位
	Providers []string // 开放的模型路由
	// Secret 返回供应商的真实密钥。闸门不缓存返回值。
	Secret func(provider string) (string, error)
	// Ask 在请求被暂扣时调用。返回 true 表示用户允许。
	// ctx 在 HoldTimeout 后取消，这时调用方必须返回 false。
	Ask     func(ctx context.Context, e Event) bool
	OnEvent func(e Event)
}

const HoldTimeout = 60 * time.Second

type Gate struct {
	cfg          Config
	host         string // 非 CONNECT 请求必须携带的 Host 头
	ln           net.Listener
	srv          *http.Server
	placeholders map[string]string
	routes       map[string]bool
	allow        map[string]bool

	// ctx 在 Close 时取消，正在进行的暂扣随之结束。
	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	decisions map[string]bool // 主机 → 用户在本次运行中的决定
	holds     map[string]*hold
}

// Start 监听 127.0.0.1 上的随机端口。
func Start(cfg Config) (*Gate, error) {
	if cfg.Secret == nil || cfg.Ask == nil {
		return nil, errors.New("gate: Config.Secret 和 Config.Ask 不能为空")
	}
	g := &Gate{
		cfg:          cfg,
		placeholders: map[string]string{},
		routes:       map[string]bool{},
		allow:        map[string]bool{},
		decisions:    map[string]bool{},
		holds:        map[string]*hold{},
	}
	for _, h := range cfg.Allow {
		g.allow[strings.ToLower(h)] = true
	}
	for _, name := range cfg.Providers {
		if _, ok := providers[name]; !ok {
			return nil, errors.New("gate: 未知的模型供应商 " + name)
		}
		g.routes[name] = true
	}
	for name := range providers {
		b := make([]byte, 16)
		rand.Read(b)
		g.placeholders[name] = "airlock-" + hex.EncodeToString(b)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	g.ln = ln
	g.ctx, g.cancel = context.WithCancel(context.Background())
	g.host = cfg.Host
	if g.host == "" {
		g.host = "127.0.0.1:" + strconv.Itoa(g.Port())
	}
	g.srv = &http.Server{Handler: g, ReadHeaderTimeout: 10 * time.Second, MaxHeaderBytes: 64 << 10}
	go g.srv.Serve(ln)
	return g, nil
}

func (g *Gate) Port() int { return g.ln.Addr().(*net.TCPAddr).Port }

// Placeholder 返回本实例为供应商签发的占位符。
func (g *Gate) Placeholder(provider string) string { return g.placeholders[provider] }

func (g *Gate) Close() error {
	g.cancel()
	return g.srv.Close()
}

// ResetDecision 撤销本次运行中对 host 的决定，并断开这个主机已建立的连接。
func (g *Gate) ResetDecision(host string) {}

func (g *Gate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, port := splitHost(r.Host)
	e := Event{Host: host, Port: port, Method: r.Method}
	if r.Method == http.MethodConnect {
		g.connect(w, r)
		return
	}
	if r.Host != g.host {
		g.deny(w, e, "bad-host", http.StatusForbidden)
		return
	}
	if name, rest, ok := route(r.URL.Path); ok && g.routes[name] {
		g.serveRoute(w, r, name, rest)
		return
	}
	g.deny(w, e, "unsupported", http.StatusNotFound)
}

// deny 记录拒绝的判定，并给沙箱返回错误。
func (g *Gate) deny(w http.ResponseWriter, e Event, rule string, status int) {
	e.Rule, e.Allowed = rule, false
	g.emit(e)
	http.Error(w, "Airlock 闸门拒绝了这个请求（"+rule+"）", status)
}

func (g *Gate) emit(e Event) {
	e.Time = time.Now()
	if g.cfg.OnEvent != nil {
		g.cfg.OnEvent(e)
	}
}

// splitHost 拆分 Host 头。没有端口时，端口记为 80。
func splitHost(hostport string) (string, int) {
	host, p, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport, 80
	}
	port, _ := strconv.Atoi(p)
	return host, port
}
