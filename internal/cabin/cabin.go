package cabin

import (
	"context"
	"errors"

	"github.com/lijiahao818/airlock/internal/audit"
	"github.com/lijiahao818/airlock/internal/gate"
	"github.com/lijiahao818/airlock/internal/sandbox"
	"github.com/lijiahao818/airlock/internal/staging"
)

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
	// Starting 表示正在启动。刷新页面后，运行页靠它继续显示启动进度。只在 List 的结果中出现，不保存。
	Starting bool `json:"starting,omitempty"`
	// Problem 是运行中发现的问题，例如出网中继已经停止。为空表示没有发现问题。只在 List 的结果中出现，不保存。
	Problem string `json:"problem,omitempty"`
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
	Cached   FindingKind = "cached"    // 依赖目录：不复制到暂存区，改用缓存卷
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
	Kind     string      `json:"kind"` // request、hold、explained、hold-closed、status、progress 或 problem
	Cabin    string      `json:"cabin"`
	Request  *gate.Event `json:"request,omitempty"`  // Kind 为 request
	Hold     *Hold       `json:"hold,omitempty"`     // Kind 为 hold、explained 或 hold-closed
	Status   Status      `json:"status,omitempty"`   // Kind 为 status
	Progress *Progress   `json:"progress,omitempty"` // Kind 为 progress
	Problem  string      `json:"problem,omitempty"`  // Kind 为 problem：沙箱或出网中继已经停止的说明
}

// Progress 是启动时复制项目文件的进度。
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
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

	// Holds 返回工作舱正在等待用户决定的暂扣。
	Holds(id string) ([]Hold, error)
	Decide(holdID string, allow bool) error
	// ResetDecision 撤销本次运行中对 host（主机:端口，例如 example.com:443）的决定，只在 Running 状态可用。
	ResetDecision(id, host string) error
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
	// Rewrite 返回 rel 复制进沙箱前的内容改写函数。返回 nil 表示原样复制。
	Rewrite(rel string) func(content []byte) []byte
	// Exit 判定改动。project 是项目文件夹，用来读取基线内容。
	Exit(project, files string, changes []staging.Change) []Finding
}

type Options struct {
	DataDir   string
	Backend   sandbox.Backend
	Secret    func(provider string) (string, error) // 传给 gate.Config.Secret
	TermURL   string                                // 网页终端的地址，例如 http://127.0.0.1:<端口>
	Explainer Explainer                             // 为 nil 时只用规则模板
	Inspector Inspector                             // 为 nil 时不排除文件，不标注风险
}

var _ Service = (*Manager)(nil)
