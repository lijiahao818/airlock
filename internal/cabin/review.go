package cabin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/lijiahao818/airlock/internal/audit"
	"github.com/lijiahao818/airlock/internal/staging"
)

// area 返回工作舱的暂存区。Running 状态用运行中的暂存区，Review 状态重新打开，所以程序重启后仍然可以审查。
func (m *Manager) area(id string) (*staging.Area, Cabin, error) {
	m.mu.Lock()
	i, err := m.index(id)
	if err != nil {
		m.mu.Unlock()
		return nil, Cabin{}, err
	}
	c, r := m.cabins[i], m.runs[id]
	m.mu.Unlock()
	switch c.Status {
	case Running:
		return r.area, c, nil
	case Review:
		a, err := staging.Open(c.Project, m.areaDir(id), nil)
		return a, c, err
	}
	return nil, c, errors.New("cabin: 工作舱是空闲状态，没有改动。请先启动工作舱。")
}

// ReviewItems 合并 Area.Changes 和 Inspector.Exit 的结果。依赖目录的改动不列出。
func (m *Manager) ReviewItems(id string) ([]ReviewItem, error) {
	a, _, err := m.area(id)
	if err != nil {
		return nil, err
	}
	return m.items(a)
}

func (m *Manager) items(a *staging.Area) ([]ReviewItem, error) {
	changes, err := a.Changes()
	if err != nil {
		return nil, err
	}
	byPath := map[string][]Finding{}
	if m.o.Inspector != nil {
		for _, f := range m.o.Inspector.Exit(a.Files(), changes) {
			byPath[f.Path] = append(byPath[f.Path], f)
		}
	}
	items := []ReviewItem{}
	for _, ch := range changes {
		fs := byPath[ch.Path]
		if slices.ContainsFunc(fs, func(f Finding) bool { return f.Kind == Cached }) {
			continue
		}
		items = append(items, ReviewItem{Change: ch, Findings: append([]Finding{}, fs...)})
	}
	return items, nil
}

// reviewArea 用 begin 检查工作舱处在 want 状态并标记为忙，返回暂存区，并确认 path 是 ReviewItems 中的一个改动。
// 依赖目录的改动不能撤销，也不能写回。没有返回错误时，调用方结束后调用 end。
func (m *Manager) reviewArea(id, path, op string, want Status) (*staging.Area, error) {
	if _, err := m.begin(id, op, want); err != nil {
		return nil, err
	}
	a, _, err := m.area(id)
	var items []ReviewItem
	if err == nil {
		items, err = m.items(a)
	}
	if err == nil && !slices.ContainsFunc(items, func(it ReviewItem) bool { return it.Path == path }) {
		err = fmt.Errorf("cabin: %q 不在改动列表中。请刷新改动列表。", path)
	}
	if err != nil {
		m.end(id)
		return nil, err
	}
	return a, nil
}

// Revert 撤销一个改动，只在 Running 状态可用。
func (m *Manager) Revert(id, path string) error {
	a, err := m.reviewArea(id, path, "撤销改动", Running)
	if err != nil {
		return err
	}
	defer m.end(id)
	return a.Revert(path)
}

// Apply 把一个改动写回项目，只在 Review 状态可用。
func (m *Manager) Apply(id, path string) error {
	a, err := m.reviewArea(id, path, "写回项目", Review)
	if err != nil {
		return err
	}
	defer m.end(id)
	return a.Apply(path)
}

// FinishReview 为每个改动写入一条审查记录，丢弃暂存区，回到 Idle。
func (m *Manager) FinishReview(id string) error {
	c, err := m.begin(id, "完成审查", Review)
	if err != nil {
		return err
	}
	defer m.end(id)
	a, err := staging.Open(c.Project, m.areaDir(id), nil)
	if err != nil {
		return err
	}
	items, err := m.items(a)
	if err != nil {
		return err
	}
	log, err := audit.Open(m.runLog(c.RunID))
	if err != nil {
		return err
	}
	for _, it := range items {
		if err := log.Add(audit.Entry{Review: &audit.Review{Time: time.Now(), Path: it.Path, Applied: it.Applied}}); err != nil {
			log.Close()
			return err
		}
	}
	if err := log.Close(); err != nil {
		return err
	}
	m.mu.Lock()
	err = m.setStatus(id, Idle, c.RunID)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	// 删除沙箱后的几秒内，Docker Desktop 仍占用暂存区目录。审查记录已经写入，
	// 所以删除失败不算审查失败，在后台重试。Airlock 退出时没删掉的，下次启动时删除。
	// 工作舱仍标记为忙，不会开始新的运行，所以删除时不持有 m.mu，不阻塞其他工作舱。
	if m.removeAll(m.areaDir(id)) != nil {
		go m.retryRemoveArea(id)
	}
	return nil
}

const removeRetries = 30

// retryRemoveArea 在后台重试删除 Idle 状态工作舱的暂存区。
func (m *Manager) retryRemoveArea(id string) {
	for range removeRetries {
		time.Sleep(m.retryDelay)
		m.mu.Lock()
		// 用户再次启动这个工作舱后停止重试，否则会删掉新的暂存区。
		// 删除前标记为忙，删除期间不能启动，所以删除时不持有 m.mu。
		i, err := m.index(id)
		if err != nil || m.busy[id] || m.cabins[i].Status != Idle {
			m.mu.Unlock()
			return
		}
		m.busy[id] = true
		m.mu.Unlock()
		err = m.removeAll(m.areaDir(id))
		m.end(id)
		if err == nil {
			return
		}
	}
}

// Report 返回最近一次运行的报告。重置后没有报告。
func (m *Manager) Report(id string) (audit.Report, error) {
	c, err := m.get(id)
	if err != nil {
		return audit.Report{}, err
	}
	if c.RunID == "" {
		return audit.Report{}, errors.New("cabin: 这个工作舱还没有运行报告。")
	}
	// 最后一行不完整时，Read 返回之前完整的条目和错误，报告用这些条目生成。一条也没有读到时返回错误。
	entries, err := audit.Read(m.runLog(c.RunID))
	if err != nil && len(entries) == 0 {
		return audit.Report{}, err
	}
	return audit.Summarize(entries), nil
}

// Reset 丢弃暂存区，删除缓存卷，回到 Idle，不生成运行报告。Running 状态时先停止。
// Idle 状态也可以重置，用来删除上次运行安装的依赖。
func (m *Manager) Reset(ctx context.Context, id string) error {
	c, err := m.begin(id, "重置", Idle, Running, Review)
	if err != nil {
		return err
	}
	defer m.end(id)
	if c.Status == Running {
		if err := m.stop(ctx, id); err != nil {
			return err
		}
	}
	if err := m.o.Backend.RemoveCache(ctx, c.ID); err != nil {
		return fmt.Errorf("cabin: 删除依赖缓存失败：%w。请确认 Docker Desktop 正在运行，然后再次重置。", err)
	}
	if err := os.RemoveAll(m.areaDir(id)); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entry, id)
	return m.setStatus(id, Idle, "")
}
