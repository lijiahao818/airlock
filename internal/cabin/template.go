package cabin

import (
	"cmp"
	"fmt"
	"net/netip"
)

// Template 用规则模板生成解释。Source 为 template。安全副驾不存在或不可用时，拦截卡片使用它。
func Template(m HoldMeta) Explanation {
	agent := cmp.Or(m.Agent, "agent")
	host := cmp.Or(m.Host, "一个未知的地址")
	e := Explanation{
		What: fmt.Sprintf("%s 想连接 %s 的 %d 端口（端口是网络服务的门牌号）。这个地址不在当前安全档位的自动放行名单中，所以 Airlock 先拦下了这个连接。",
			agent, host, m.Port),
		RiskLevel: "medium",
		Risk:      "agent 可能在下载依赖或查资料，也可能把项目代码或数据发给这个地址。Airlock 看不到连接里传了什么。",
		Action:    "如果你认识这个地址，并且它和当前任务有关，点击“允许”，本次运行中不再询问。不确定时点击“拒绝”，agent 只会收到连接失败。60 秒内不选择，Airlock 自动拒绝。",
		Source:    "template",
	}
	switch {
	case m.Port != 443:
		e.RiskLevel = "high"
		e.Risk = "这个连接没有使用网站常用的 443 端口。下载依赖和查资料很少用别的端口，它更可能是在连接不明的服务。Airlock 看不到连接里传了什么。"
	case isIP(m.Host):
		e.RiskLevel = "high"
		e.Risk = "agent 直接连接一个 IP 地址（网络上的数字地址），没有使用网站名称。正常的网站很少这样访问，这常见于把数据发给临时搭建的服务器。Airlock 看不到连接里传了什么。"
	}
	return e
}

func isIP(host string) bool {
	_, err := netip.ParseAddr(host)
	return err == nil
}
