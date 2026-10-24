package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lijiahao818/airlock/internal/gate"
)

type Entry struct {
	Gate   *gate.Event `json:"gate,omitempty"`   // 一次出网判定
	Review *Review     `json:"review,omitempty"` // 一个出舱审查决定
}

type Review struct {
	Time    time.Time `json:"time"`
	Path    string    `json:"path"`
	Applied bool      `json:"applied"`
}

// Log 是一次运行的审计记录。闸门会从多个连接同时调用 Add，所以写入要加锁。
type Log struct {
	mu sync.Mutex
	f  *os.File
}

// Open 打开 JSONL 文件，在末尾追加。文件和上级目录不存在时创建它们。
func Open(path string) (*Log, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	return &Log{f: f}, nil
}

// Add 写入一行 JSON。一行只用一次 Write，并发写入的行不会交错。
func (l *Log) Add(e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return os.ErrClosed
	}
	_, err = l.f.Write(append(line, '\n'))
	return err
}

// Close 关闭文件。可以重复调用，第二次起返回 nil，所以可以和 defer 一起使用。
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// maxLine 是一行审计记录的长度上限。事件只有元数据，正常的行远短于这个值。
const maxLine = 1 << 20

// Read 读出全部条目。空行跳过。格式错误的行返回错误，错误信息中有行号。
// 出错时也返回出错前读到的条目。进程崩溃可能留下半行，调用方仍可以用前面完整的条目生成报告。
func Read(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries := []Entry{}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64<<10), maxLine)
	for n := 1; s.Scan(); n++ {
		if len(s.Bytes()) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(s.Bytes(), &e); err != nil {
			return entries, fmt.Errorf("audit: 第 %d 行格式错误：%w", n, err)
		}
		entries = append(entries, e)
	}
	if err := s.Err(); err != nil {
		return entries, fmt.Errorf("audit: 读取审计记录失败：%w", err)
	}
	return entries, nil
}

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

// Summarize 汇总一次运行的条目。主机按第一次出现的顺序排列，暂扣和审查决定按记录顺序排列。
// 结果中的切片和 map 都不是 nil，JSON 中输出 [] 和 {}。
func Summarize(entries []Entry) Report {
	r := Report{Hosts: []HostSummary{}, Holds: []HoldSummary{}, KeyUses: map[string]int{}, Files: []Review{}}
	index := map[string]int{} // 主机 → Hosts 中的下标
	for _, e := range entries {
		if e.Review != nil {
			r.Files = append(r.Files, *e.Review)
		}
		g := e.Gate
		if g == nil {
			continue
		}
		i, ok := index[g.Host]
		if !ok {
			i = len(r.Hosts)
			index[g.Host] = i
			r.Hosts = append(r.Hosts, HostSummary{Host: g.Host})
		}
		if g.Allowed {
			r.Hosts[i].Allowed++
		} else {
			r.Hosts[i].Denied++
		}
		switch g.Rule {
		case "user-allow", "user-deny", "timeout":
			r.Holds = append(r.Holds, HoldSummary{Host: g.Host, Rule: g.Rule})
		case "route":
			if g.Allowed && g.Provider != "" {
				r.KeyUses[g.Provider]++
			}
		}
	}
	return r
}
