package advisor

import (
	"context"
	"errors"

	"github.com/lijiahao818/airlock/internal/cabin"
)

var errNotImplemented = errors.New("advisor: 未实现")

type Settings struct {
	Enabled     bool   `json:"enabled"`     // 用户开启了安全副驾
	Provider    string `json:"provider"`    // 用户添加了两家的密钥时选择的一家
	NoticeShown bool   `json:"noticeShown"` // 用户已经看过数据发送说明
}

type Config struct {
	DataDir     string
	BaseURL     string                       // 安全副驾专用闸门的地址
	Placeholder func(provider string) string // 安全副驾专用闸门签发的占位符
	HasKey      func(provider string) bool
}

type Advisor struct{}

var _ cabin.Explainer = (*Advisor)(nil)

func New(cfg Config) (*Advisor, error) { return nil, errNotImplemented }

func (a *Advisor) Settings() Settings { return Settings{} }

func (a *Advisor) SetSettings(s Settings) error { return errNotImplemented }

func (a *Advisor) Explain(ctx context.Context, runID string, m cabin.HoldMeta) (cabin.Explanation, error) {
	return cabin.Explanation{}, errNotImplemented
}
