package cabin

import (
	"context"
	"errors"

	"github.com/lijiahao818/airlock/internal/audit"
	"github.com/lijiahao818/airlock/internal/gate"
	"github.com/lijiahao818/airlock/internal/sandbox"
	"github.com/lijiahao818/airlock/internal/staging"
)

var errNotImplemented = errors.New("cabin: 未实现")

type Status string

const (
	Idle    Status = "idle"    // 空闲
	Running Status = "running" // 运行中
	Review  Status = "review"  // 等待出舱审查
)

type Cabin struct {
	ID      string `json:"id"`
	Name    string `json:"name"` // 项目文件夹的名称
	Project string `json:"project"`
	Preset  string `json:"preset"`
	Tier    string `json:"tier"`
	Status  Status `json:"status"`
	RunID   string `json:"runId,omitempty"` // 当前或最近一次运行
}

// HoldMeta 是安全副驾可以看到的全部数据。
type HoldMeta struct {
	Agent  string `json:"agent"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Method string `json:"method"`
	Path   string `json:"path,omitempty"`
	Rule   string `json:"rule"`
}

type Explanation struct {
	What          string `json:"what"`                    // 发生了什么
	Risk          string `json:"risk"`                    // 风险多大
	RiskLevel     string `json:"riskLevel"`               // low、medium 或 high
	Action        string `json:"action"`                  // 用户可以怎么做
	SuggestAllow  bool   `json:"suggestAllow"`            // 只用于显示，不影响判定
	Source        string `json:"source"`                  // template 或 ai
	AIUnavailable bool   `json:"aiUnavailable"`           // 安全副驾已开启，但这次没有生成 AI 解释
	PromptVersion string `json:"promptVersion,omitempty"` // Source 为 ai 时，生成解释的提示词版本
}

type Hold struct {
	ID    string `json:"id"`
	Cabin string `json:"cabin"`
	RunID string `json:"runId"`
	HoldMeta
	Explanation Explanation `json:"explanation"`
}

type FindingKind string

const (
	Excluded FindingKind = "excluded"  // 不复制到暂存区
	Notice   FindingKind = "notice"    // 需要注意
	HighRisk FindingKind = "high-risk" // 高风险改动
)

type Finding struct {
	Path   string      `json:"path"`
	Kind   FindingKind `json:"kind"`
	Reason string      `json:"reason"` // 给用户看的大白话
}

type ReviewItem struct {
	staging.Change
	Findings []Finding `json:"findings"`
}

type Event struct {
	Kind    string      `json:"kind"` // request、hold、explained、hold-closed 或 status
	Cabin   string      `json:"cabin"`
	Request *gate.Event `json:"request,omitempty"` // Kind 为 request
	Hold    *Hold       `json:"hold,omitempty"`    // Kind 为 hold、explained 或 hold-closed
	Status  Status      `json:"status,omitempty"`  // Kind 为 status
}

// Service 是界面层使用的全部功能。
type Service interface {
	CheckDocker(ctx context.Context) error

	List() ([]Cabin, error)
	Create(project, preset, tier string) (Cabin, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Reset(ctx context.Context, id string) error

	// AgentURL 返回嵌入运行页面的 agent 界面地址。
	AgentURL(id string) (string, error)
	// OpenTerminal 是保底方式：在独立终端窗口中打开 agent。
	OpenTerminal(id string) error
	// Attach 供网页终端使用。token 是本次运行的终端令牌。
	Attach(ctx context.Context, id, token string) (sandbox.Session, error)

	EntryFindings(id string) ([]Finding, error)
	// ReviewItems 在 Running 和 Review 状态都可以调用。
	ReviewItems(id string) ([]ReviewItem, error)
	// Revert 撤销一个改动，只在 Running 状态可用。
	Revert(id, path string) error
	// Apply 把一个改动写回项目，只在 Review 状态可用。
	Apply(id, path string) error
	FinishReview(id string) error
	// Report 返回最近一次运行的报告。
	Report(id string) (audit.Report, error)

	Decide(holdID string, allow bool) error
	Subscribe() (events <-chan Event, cancel func())
}

var ErrAdvisorOff = errors.New("安全副驾已关闭")

// Explainer 由 advisor 实现。安全副驾关闭时返回 ErrAdvisorOff。
// cabin 在 5 秒后取消 ctx。
type Explainer interface {
	Explain(ctx context.Context, runID string, m HoldMeta) (Explanation, error)
}

// Inspector 由 filecheck 实现。
type Inspector interface {
	Entry(project string) ([]Finding, error)
	Exit(files string, changes []staging.Change) []Finding
}

type Options struct {
	DataDir   string
	Backend   sandbox.Backend
	Secret    func(provider string) (string, error) // 传给 gate.Config.Secret
	TermURL   string                                // 网页终端的地址，例如 http://127.0.0.1:<端口>
	Explainer Explainer                             // 为 nil 时只用规则模板
	Inspector Inspector                             // 为 nil 时不排除文件，不标注风险
}

type Manager struct{}

var _ Service = (*Manager)(nil)

func New(o Options) (*Manager, error) { return nil, errNotImplemented }

// Template 用规则模板生成解释。Source 为 template。
func Template(m HoldMeta) Explanation { return Explanation{Source: "template"} }

func (m *Manager) CheckDocker(ctx context.Context) error { return errNotImplemented }

func (m *Manager) List() ([]Cabin, error) { return nil, errNotImplemented }

func (m *Manager) Create(project, preset, tier string) (Cabin, error) {
	return Cabin{}, errNotImplemented
}

func (m *Manager) Start(ctx context.Context, id string) error { return errNotImplemented }

func (m *Manager) Stop(ctx context.Context, id string) error { return errNotImplemented }

func (m *Manager) Reset(ctx context.Context, id string) error { return errNotImplemented }

func (m *Manager) AgentURL(id string) (string, error) { return "", errNotImplemented }

func (m *Manager) OpenTerminal(id string) error { return errNotImplemented }

func (m *Manager) Attach(ctx context.Context, id, token string) (sandbox.Session, error) {
	return nil, errNotImplemented
}

func (m *Manager) EntryFindings(id string) ([]Finding, error) { return nil, errNotImplemented }

func (m *Manager) ReviewItems(id string) ([]ReviewItem, error) { return nil, errNotImplemented }

func (m *Manager) Revert(id, path string) error { return errNotImplemented }

func (m *Manager) Apply(id, path string) error { return errNotImplemented }

func (m *Manager) FinishReview(id string) error { return errNotImplemented }

func (m *Manager) Report(id string) (audit.Report, error) {
	return audit.Report{}, errNotImplemented
}

func (m *Manager) Decide(holdID string, allow bool) error { return errNotImplemented }

func (m *Manager) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event)
	close(ch)
	return ch, func() {}
}
