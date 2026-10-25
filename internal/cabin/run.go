package cabin

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lijiahao818/airlock/internal/audit"
	"github.com/lijiahao818/airlock/internal/desktop"
	"github.com/lijiahao818/airlock/internal/gate"
	"github.com/lijiahao818/airlock/internal/sandbox"
	"github.com/lijiahao818/airlock/internal/staging"
	"github.com/lijiahao818/airlock/presets"
)

// run 是一次正在进行的运行。Stop 时删除，终端令牌随之失效。
type run struct {
	id       string
	preset   presets.Preset
	area     *staging.Area
	log      *audit.Log
	gate     *gate.Gate
	box      string // 沙箱 ID
	token    string // 网页终端的令牌
	agentURL string
}

const (
	gateHost = "airlock-gate:8080" // 沙箱通过出网中继访问闸门的地址
	// tokenTimeout 内没有在启动输出中找到网页令牌时，启动失败。
	tokenTimeout = 2 * time.Minute
	// maxLogLines 限制读取的启动输出行数。启动输出来自沙箱，是不可信数据。
	maxLogLines = 1000
)

// cacheDirs 是项目根目录下总是使用缓存卷的依赖目录，见 docs/architecture.md 第 6.1 节。
var cacheDirs = []string{"node_modules", ".venv"}

// webTokenRe 是从启动输出中提取的令牌必须满足的格式。令牌会拼进网址，所以只允许 URL 安全的字符。
var webTokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]{16,256}$`)

// openTerminal 打开独立的终端窗口。单元测试替换它，不打开真实的窗口。
var openTerminal = desktop.OpenTerminal

func (m *Manager) areaDir(id string) string { return filepath.Join(m.o.DataDir, "cabins", id) }

func (m *Manager) runLog(runID string) string {
	return filepath.Join(m.o.DataDir, "runs", runID+".jsonl")
}

// Start 按 docs/architecture.md 第 6.1 节的顺序启动一次运行。任何一步失败时，清理已创建的资源，状态仍为 Idle。
func (m *Manager) Start(ctx context.Context, id string) error {
	c, err := m.begin(id, "启动", Idle)
	if err != nil {
		return err
	}
	defer m.end(id)
	m.mu.Lock()
	m.starts[id] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.starts, id)
		m.mu.Unlock()
	}()
	r, err := m.start(ctx, c)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[id] = r
	return m.setStatus(id, Running, r.id)
}

func (m *Manager) start(ctx context.Context, c Cabin) (_ *run, err error) {
	var undo []func()
	defer func() {
		if err != nil {
			slices.Reverse(undo)
			for _, f := range undo {
				f()
			}
		}
	}()

	p, err := presets.Get(c.Preset)
	if err != nil {
		return nil, err
	}
	findings := []Finding{}
	if m.o.Inspector != nil {
		if findings, err = m.o.Inspector.Entry(c.Project); err != nil {
			return nil, fmt.Errorf("cabin: 进舱检查失败：%w", err)
		}
	}
	m.mu.Lock()
	m.entry[c.ID] = findings
	m.mu.Unlock()
	undo = append(undo, func() {
		m.mu.Lock()
		delete(m.entry, c.ID)
		m.mu.Unlock()
	})

	skip, volumes := entryPlan(findings)
	dir := m.areaDir(c.ID)
	// 上次启动失败或程序异常退出时，可能留下暂存区。工作舱是 Idle 状态，暂存区中没有需要审查的改动。
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	area, err := staging.Create(c.Project, dir, skip, func(done, total int) {
		m.mu.Lock()
		m.publish(Event{Kind: "progress", Cabin: c.ID, Progress: &Progress{Done: done, Total: total}})
		m.mu.Unlock()
	})
	if err != nil {
		return nil, fmt.Errorf("cabin: 复制项目文件失败：%w", err)
	}
	undo = append(undo, func() { area.Discard() })

	r := &run{id: newID(), preset: p, area: area, token: randomToken()}
	if r.log, err = audit.Open(m.runLog(r.id)); err != nil {
		return nil, err
	}
	// 启动失败时删除审计记录文件，runs 目录中不留下空记录。
	undo = append(undo, func() {
		r.log.Close()
		os.Remove(m.runLog(r.id))
	})

	hosts, err := gate.TierHosts(c.Tier)
	if err != nil {
		return nil, err
	}
	r.gate, err = gate.Start(gate.Config{
		Host:      gateHost,
		Allow:     hosts,
		Providers: []string{p.Provider},
		Secret:    m.o.Secret,
		Ask:       m.ask(c.ID, r.id, p.Name),
		OnEvent:   m.onEvent(c.ID, r.log),
	})
	if err != nil {
		return nil, err
	}
	undo = append(undo, func() { r.gate.Close() })

	hostPort := 0
	if p.UI == "web" {
		if hostPort, err = freePort(); err != nil {
			return nil, err
		}
	}
	spec := m.spec(c, p, r, area.Files(), hostPort, volumes)
	if r.box, err = m.o.Backend.Create(ctx, spec); err != nil {
		return nil, fmt.Errorf("cabin: 创建沙箱失败：%w", err)
	}
	undo = append(undo, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		m.o.Backend.Remove(ctx, r.box)
	})

	if p.UI == "web" {
		token, err := m.webToken(ctx, r.box, p.Web.TokenPattern)
		if err != nil {
			return nil, err
		}
		// 主机和端口由 cabin 拼接，不取自启动输出。
		r.agentURL = "http://127.0.0.1:" + strconv.Itoa(hostPort) + strings.ReplaceAll(p.Web.OpenPath, "{token}", url.QueryEscape(token))
	} else {
		r.agentURL = m.o.TermURL + "/?cabin=" + url.QueryEscape(c.ID) + "#token=" + r.token
	}
	return r, nil
}

// entryPlan 由进舱检查的结果得出不复制的路径和缓存卷目录。
func entryPlan(findings []Finding) (skip func(rel string) bool, volumes []string) {
	excluded := map[string]bool{}
	volumes = slices.Clone(cacheDirs)
	for _, f := range findings {
		switch f.Kind {
		case Excluded:
			excluded[f.Path] = true
		case Cached:
			dir := strings.TrimSuffix(f.Path, "/")
			excluded[dir] = true
			volumes = append(volumes, dir)
		}
	}
	slices.Sort(volumes)
	return func(rel string) bool { return excluded[rel] }, slices.Compact(volumes)
}

// spec 展开预设中的模板变量。Env 和 Files 中只有占位符和地址，真实密钥只在闸门中。
func (m *Manager) spec(c Cabin, p presets.Preset, r *run, files string, hostPort int, volumes []string) sandbox.Spec {
	ph := r.gate.Placeholder(p.Provider)
	vars := strings.NewReplacer(
		"{gate}", "http://"+gateHost,
		"{placeholder}", ph,
		"{placeholder-tail}", ph[max(0, len(ph)-20):],
		"{webhost}", "127.0.0.1:"+strconv.Itoa(hostPort),
	)
	expand := func(in map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range in {
			out[k] = vars.Replace(v)
		}
		return out
	}
	s := sandbox.Spec{
		Name:       c.ID,
		Image:      imageName(p),
		Dockerfile: p.Dockerfile,
		StagingDir: files,
		Env:        expand(p.Env),
		Files:      expand(p.Files),
		GatePort:   r.gate.Port(),
		Volumes:    volumes,
	}
	// terminal 型的 agent 由 Exec 在 tmux 会话中启动，容器的主进程是空闲进程。
	if p.UI == "web" {
		for _, a := range p.Cmd {
			s.Cmd = append(s.Cmd, vars.Replace(a))
		}
		s.WebPort, s.HostPort = p.Web.Port, hostPort
	}
	return s
}

// imageName 含 Dockerfile 的哈希。Dockerfile 改变后使用新的镜像名，sandbox 会重新构建。
func imageName(p presets.Preset) string {
	sum := sha256.Sum256(p.Dockerfile)
	return "airlock-" + p.ID + ":" + hex.EncodeToString(sum[:6])
}

func randomToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// webToken 逐行读取启动输出，用 pattern 提取第一个匹配的令牌，并校验格式。
func (m *Manager) webToken(ctx context.Context, box, pattern string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	// presets 已经要求只有一个分组。这里再检查一次，下面的 mt[1] 不依赖其他模块的校验。
	if re.NumSubexp() != 1 {
		return "", errors.New("cabin: 预设的 tokenPattern 必须只有一个分组")
	}
	ctx, cancel := context.WithTimeout(ctx, tokenTimeout)
	defer cancel()
	logs, err := m.o.Backend.Logs(ctx, box)
	if err != nil {
		return "", err
	}
	defer logs.Close()
	sc := bufio.NewScanner(logs) // ScanLines 去掉行尾的 \r\n 或 \n
	for n := 0; n < maxLogLines && sc.Scan(); n++ {
		if mt := re.FindStringSubmatch(sc.Text()); mt != nil {
			if !webTokenRe.MatchString(mt[1]) {
				return "", errors.New("cabin: agent 输出的网页令牌格式不对，Airlock 没有打开网页界面。请重置工作舱后再试。")
			}
			return mt[1], nil
		}
	}
	return "", errors.New("cabin: 没有在 agent 的启动输出中找到网页令牌。请检查 Docker Desktop 是否正常，然后重新启动。")
}

// Stop 删除沙箱，关闭闸门和审计记录，让终端令牌失效，进入出舱审查。删除沙箱失败时状态不变，可以再次停止。
func (m *Manager) Stop(ctx context.Context, id string) error {
	if _, err := m.begin(id, "停止", Running); err != nil {
		return err
	}
	defer m.end(id)
	return m.stop(ctx, id)
}

// StopAll 停止全部正在运行的工作舱，由程序退出时调用。这些工作舱进入出舱审查。
// 工作舱并发停止，ctx 到期时没有停完的沙箱由下次启动时的 Prune 删除。
func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	var ids []string
	for id := range m.runs {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	errs := make([]error, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Go(func() { errs[i] = m.Stop(ctx, id) })
	}
	wg.Wait()
	return errors.Join(errs...)
}

// stop 由 Stop 和 Reset 调用。调用方已经用 begin 标记工作舱为忙。
func (m *Manager) stop(ctx context.Context, id string) error {
	// Start 先放入 m.runs 再改为 Running，New 把遗留的 Running 改为 Review，所以 Running 状态一定有运行。
	m.mu.Lock()
	r := m.runs[id]
	m.mu.Unlock()
	if err := m.o.Backend.Remove(ctx, r.box); err != nil {
		return fmt.Errorf("cabin: 删除沙箱失败：%w。请确认 Docker Desktop 正在运行，然后再次停止。", err)
	}
	// 关闭闸门会结束正在进行的暂扣。
	r.gate.Close()
	r.log.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runs, id)
	return m.setStatus(id, Review, r.id)
}

// running 返回正在进行的运行。
func (m *Manager) running(id string) (*run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.index(id); err != nil {
		return nil, err
	}
	if r := m.runs[id]; r != nil {
		return r, nil
	}
	return nil, errors.New("cabin: 工作舱没有在运行。请先启动工作舱。")
}

func (m *Manager) AgentURL(id string) (string, error) {
	r, err := m.running(id)
	if err != nil {
		return "", err
	}
	return r.agentURL, nil
}

func (m *Manager) OpenTerminal(id string) error {
	r, err := m.running(id)
	if err != nil {
		return err
	}
	if r.preset.UI != "terminal" {
		return errors.New("cabin: 这个 agent 使用网页界面，没有终端。请在运行页面中使用它。")
	}
	argv := m.o.Backend.AttachCommand(r.box, r.preset.Cmd)
	if len(argv) == 0 {
		return errors.New("cabin: 无法生成连接 agent 的命令。请重新启动工作舱。")
	}
	return openTerminal(argv)
}

// Attach 用常量时间比较终端令牌。令牌不对或运行已结束时，不调用 Backend.Exec。
func (m *Manager) Attach(ctx context.Context, id, token string) (sandbox.Session, error) {
	r, err := m.running(id)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(r.token)) != 1 {
		return nil, errors.New("cabin: 终端令牌无效。请从控制台重新打开运行页面。")
	}
	if r.preset.UI != "terminal" {
		return nil, errors.New("cabin: 这个 agent 使用网页界面，没有终端。")
	}
	return m.o.Backend.Exec(ctx, r.box, r.preset.Cmd)
}

func (m *Manager) EntryFindings(id string) ([]Finding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.index(id); err != nil {
		return nil, err
	}
	return append([]Finding{}, m.entry[id]...), nil
}
