package audit

import (
	"errors"
	"time"

	"github.com/lijiahao818/airlock/internal/gate"
)

var errNotImplemented = errors.New("audit: 未实现")

type Entry struct {
	Gate   *gate.Event `json:"gate,omitempty"`   // 一次出网判定
	Review *Review     `json:"review,omitempty"` // 一个出舱审查决定
}

type Review struct {
	Time    time.Time `json:"time"`
	Path    string    `json:"path"`
	Applied bool      `json:"applied"`
}

type Log struct{}

// Open 打开 JSONL 文件，在末尾追加。
func Open(path string) (*Log, error) { return nil, errNotImplemented }

func (l *Log) Add(e Entry) error { return errNotImplemented }

func (l *Log) Close() error { return errNotImplemented }

func Read(path string) ([]Entry, error) { return nil, errNotImplemented }

type Report struct {
	Hosts   []HostSummary  `json:"hosts"`   // agent 访问过的主机
	Holds   []HoldSummary  `json:"holds"`   // 暂扣过的主机和用户的决定
	KeyUses map[string]int `json:"keyUses"` // 供应商 → 换上真实密钥的次数
	Files   []Review       `json:"files"`   // 改动的文件和用户的决定
}

type HostSummary struct {
	Host    string `json:"host"`
	Allowed int    `json:"allowed"`
	Denied  int    `json:"denied"`
}

type HoldSummary struct {
	Host string `json:"host"`
	Rule string `json:"rule"` // user-allow、user-deny 或 timeout
}

func Summarize(entries []Entry) Report { return Report{} }
