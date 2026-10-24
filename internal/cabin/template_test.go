package cabin

import (
	"strings"
	"testing"
)

func TestTemplate(t *testing.T) {
	cases := []struct {
		meta HoldMeta
		risk string
	}{
		{HoldMeta{Agent: "Claude Code", Host: "registry.example", Port: 443, Method: "CONNECT", Rule: "hold"}, "medium"},
		{HoldMeta{Agent: "Codex", Host: "evil.example", Port: 8443, Method: "CONNECT", Rule: "hold"}, "high"},
		{HoldMeta{Agent: "Codex", Host: "203.0.113.7", Port: 443, Method: "CONNECT", Rule: "hold"}, "high"},
		{HoldMeta{Agent: "Codex", Host: "2001:db8::1", Port: 443, Method: "CONNECT", Rule: "hold"}, "high"},
		// 元数据不全时也给出完整的解释。
		{HoldMeta{}, "high"},
	}
	for _, c := range cases {
		e := Template(c.meta)
		if e.What == "" || e.Risk == "" || e.Action == "" || e.RiskLevel != c.risk {
			t.Errorf("Template(%+v) = %+v，风险应该是 %s", c.meta, e, c.risk)
		}
		if e.Source != "template" || e.SuggestAllow || e.AIUnavailable || e.PromptVersion != "" {
			t.Errorf("Template(%+v) 的来源字段不对：%+v", c.meta, e)
		}
		if c.meta.Host != "" && !strings.Contains(e.What, c.meta.Host) {
			t.Errorf("What 应该写出主机：%q", e.What)
		}
	}
	if e := Template(HoldMeta{}); !strings.Contains(e.What, "agent") || !strings.Contains(e.What, "未知") {
		t.Errorf("缺少 agent 和主机时 What = %q", e.What)
	}
}
