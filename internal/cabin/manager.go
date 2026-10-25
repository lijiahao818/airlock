package cabin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/lijiahao818/airlock/internal/gate"
	"github.com/lijiahao818/airlock/presets"
)

// Manager 实现 Service。工作舱列表保存在 DataDir/cabins.json。
type Manager struct {
	o Options

	mu     sync.Mutex
	cabins []Cabin
	runs   map[string]*run      // 工作舱 ID → 正在进行的运行
	busy   map[string]bool      // 正在启动、停止、重置或结束审查的工作舱
	starts map[string]bool      // 正在启动的工作舱
	entry  map[string][]Finding // 工作舱 ID → 最近一次进舱检查的结果
	holds  map[string]*pending  // 暂扣 ID → 等待用户决定的暂扣
	subs   map[chan Event]struct{}
	// StopAll 已经开始。之后的 Start 返回错误，否则新的沙箱在 Airlock 退出后继续运行。
	closing bool

	// 删除暂存区的函数和重试间隔。测试替换它们，模拟暂存区被占用。
	removeAll  func(string) error
	retryDelay time.Duration
}

// subBuffer 是每个订阅者的事件缓冲。缓冲满时丢弃事件，闸门不会因为界面读得慢而卡住。
const subBuffer = 256

// New 先调用 Backend.Prune，再把 Running 状态的工作舱改为 Review。
func New(o Options) (*Manager, error) {
	if o.Backend == nil || o.Secret == nil {
		return nil, errors.New("cabin: Options.Backend 和 Options.Secret 不能为空")
	}
	if !filepath.IsAbs(o.DataDir) {
		return nil, errors.New("cabin: Options.DataDir 必须是绝对路径")
	}
	if err := os.MkdirAll(o.DataDir, 0o700); err != nil {
		return nil, err
	}
	m := &Manager{
		o:      o,
		runs:   map[string]*run{},
		busy:   map[string]bool{},
		starts: map[string]bool{},
		entry:  map[string][]Finding{},
		holds:  map[string]*pending{},
		subs:   map[chan Event]struct{}{},

		removeAll:  os.RemoveAll,
		retryDelay: time.Second,
	}
	if err := m.load(); err != nil {
		return nil, err
	}
	// Docker 没有运行时 Prune 会失败。这时上次的容器也没有在运行，残留的资源在下次 Create 同名沙箱时删除，所以继续启动。
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	m.o.Backend.Prune(ctx)
	cancel()
	// 上次程序退出时还在运行的工作舱，沙箱已经删除，暂存区还在，所以进入出舱审查。
	changed := false
	for i := range m.cabins {
		if m.cabins[i].Status == Running {
			m.cabins[i].Status = Review
			changed = true
		}
	}
	if changed {
		if err := m.save(); err != nil {
			return nil, err
		}
	}
	m.pruneAreas()
	return m, nil
}

// pruneAreas 删除残留的暂存区。只有 Review 状态的工作舱还需要暂存区。删除失败时下次启动再删。
func (m *Manager) pruneAreas() {
	entries, _ := os.ReadDir(filepath.Join(m.o.DataDir, "cabins"))
	for _, e := range entries {
		if !slices.ContainsFunc(m.cabins, func(c Cabin) bool { return c.ID == e.Name() && c.Status == Review }) {
			os.RemoveAll(m.areaDir(e.Name()))
		}
	}
}

func (m *Manager) cabinsFile() string { return filepath.Join(m.o.DataDir, "cabins.json") }

func (m *Manager) load() error {
	data, err := os.ReadFile(m.cabinsFile())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &m.cabins)
}

// save 写入临时文件后再改名，写到一半时程序退出不会损坏原来的列表。调用方持有 m.mu。
func (m *Manager) save() error {
	data, _ := json.MarshalIndent(m.cabins, "", "  ") // Cabin 只有字符串字段，编码不会失败
	tmp := m.cabinsFile() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.cabinsFile())
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (m *Manager) CheckDocker(ctx context.Context) error { return m.o.Backend.Check(ctx) }

func (m *Manager) List() ([]Cabin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := append([]Cabin{}, m.cabins...)
	for i := range list {
		list[i].Starting = m.starts[list[i].ID]
	}
	return list, nil
}

// Create 校验项目文件夹、预设和档位，新建一个 Idle 状态的工作舱。
func (m *Manager) Create(project, preset, tier string) (Cabin, error) {
	if !filepath.IsAbs(project) {
		return Cabin{}, fmt.Errorf("cabin: 项目文件夹必须是完整路径：%q。请重新选择文件夹。", project)
	}
	project = filepath.Clean(project)
	if fi, err := os.Stat(project); err != nil || !fi.IsDir() {
		return Cabin{}, fmt.Errorf("cabin: 找不到项目文件夹 %q。请重新选择文件夹。", project)
	}
	// 项目包含 Airlock 的数据目录时，其他工作舱的暂存区和审计记录会被复制进沙箱。
	if within(project, m.o.DataDir) || within(m.o.DataDir, project) {
		return Cabin{}, fmt.Errorf("cabin: 项目文件夹 %q 包含 Airlock 自己的数据，不能使用。请选择具体的项目文件夹。", project)
	}
	if _, err := presets.Get(preset); err != nil {
		return Cabin{}, fmt.Errorf("cabin: 没有这个 agent：%q", preset)
	}
	if !slices.Contains(gate.Tiers(), tier) {
		return Cabin{}, fmt.Errorf("cabin: 没有这个安全档位：%q", tier)
	}
	c := Cabin{ID: newID(), Name: filepath.Base(project), Project: project, Preset: preset, Tier: tier, Status: Idle}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cabins = append(m.cabins, c)
	if err := m.save(); err != nil {
		m.cabins = m.cabins[:len(m.cabins)-1]
		return Cabin{}, err
	}
	return c, nil
}

// within 判断 child 是否就是 parent 或在 parent 之内。Windows 上 filepath.Rel 不区分大小写。
func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

// index 返回工作舱在列表中的位置。调用方持有 m.mu。
func (m *Manager) index(id string) (int, error) {
	i := slices.IndexFunc(m.cabins, func(c Cabin) bool { return c.ID == id })
	if i < 0 {
		return 0, fmt.Errorf("cabin: 没有找到工作舱 %q。请刷新页面。", id)
	}
	return i, nil
}

func (m *Manager) get(id string) (Cabin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, err := m.index(id)
	if err != nil {
		return Cabin{}, err
	}
	return m.cabins[i], nil
}

var statusText = map[Status]string{Idle: "空闲", Running: "运行中", Review: "等待出舱审查"}

var nextStep = map[Status]string{Idle: "请先启动工作舱。", Running: "请先停止运行。", Review: "请先完成出舱审查，或者重置工作舱。"}

// begin 检查工作舱处在 allowed 中的一个状态，并标记为忙。操作结束时调用 end。
func (m *Manager) begin(id, op string, allowed ...Status) (Cabin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, err := m.index(id)
	if err != nil {
		return Cabin{}, err
	}
	if m.busy[id] {
		return Cabin{}, errors.New("cabin: 工作舱正在处理上一个操作。请稍等几秒再试。")
	}
	c := m.cabins[i]
	if err := checkStatus(c, op, allowed...); err != nil {
		return Cabin{}, err
	}
	m.busy[id] = true
	return c, nil
}

func checkStatus(c Cabin, op string, allowed ...Status) error {
	if slices.Contains(allowed, c.Status) {
		return nil
	}
	return fmt.Errorf("cabin: 工作舱现在是“%s”状态，不能%s。%s", statusText[c.Status], op, nextStep[c.Status])
}

func (m *Manager) end(id string) {
	m.mu.Lock()
	delete(m.busy, id)
	m.mu.Unlock()
}

// setStatus 修改状态，保存列表，发出 status 事件。调用方持有 m.mu。
// 调用 setStatus 时，沙箱已经创建或删除，所以保存失败时不回退内存中的状态，否则状态与实际的资源不一致。
func (m *Manager) setStatus(id string, s Status, runID string) error {
	i, err := m.index(id)
	if err != nil {
		return err
	}
	m.cabins[i].Status, m.cabins[i].RunID = s, runID
	m.publish(Event{Kind: "status", Cabin: id, Status: s})
	if err := m.save(); err != nil {
		return fmt.Errorf("cabin: 操作已经完成，但保存工作舱列表失败：%w。请检查磁盘空间。重启 Airlock 前，工作舱的状态可能显示不对。", err)
	}
	return nil
}

// publish 把事件发给每个订阅者。调用方持有 m.mu，所以事件的顺序与状态变化的顺序一致。
func (m *Manager) publish(e Event) {
	for ch := range m.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func (m *Manager) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subBuffer)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if _, ok := m.subs[ch]; ok {
			delete(m.subs, ch)
			close(ch)
		}
	}
}
