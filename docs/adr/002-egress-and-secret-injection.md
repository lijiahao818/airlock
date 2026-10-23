# ADR-002 出网只经过闸门，密钥注入不解密 HTTPS

## 状态

已接受。

## 背景

沙箱需要访问模型接口和包管理源。真实密钥只能存于系统钥匙串和闸门进程内存。闸门运行在 Windows 宿主机上。

Docker 的 internal 网络没有通往外部的路由，也无法访问宿主机。沙箱需要一条只通往闸门的路径。

## 决策

### 出网链路

```
沙箱 ──(internal 网络)──► 中继容器 ──► 宿主机上的闸门 ──► 公网
```

- 沙箱只接入 internal 网络。
- 中继容器同时接入 internal 网络和普通网络。中继容器只把流量转发到闸门的端口，不持有密钥。
- 闸门只监听 `127.0.0.1`。
- 每个沙箱使用独立的中继和闸门端口。闸门据此区分请求来自哪个沙箱。

### 密钥注入

- 模型请求：agent 的接口地址指向闸门的固定路由，例如 `/anthropic`。agent 的密钥设为占位符。闸门校验占位符，换上真实密钥，再用 HTTPS 发往该路由对应的模型接口。
- 其他请求：沙箱通过 HTTP 代理的 CONNECT 方法出网。闸门按安全档位检查目标主机。闸门不解密 HTTPS 内容。

### agent 接入方式

| agent | 接入方式 |
|---|---|
| Claude Code | 设置 `ANTHROPIC_BASE_URL` 和 `ANTHROPIC_API_KEY`。设置 `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`，关闭非必要的出网请求。 |
| Codex | 在沙箱内的 `~/.codex/config.toml` 中定义自定义模型供应商。配置文件只写环境变量名，不写密钥。关闭 WebSocket 和统计上报。 |
| DeepSeek Harness | 设置 `DEEPSEEK_BASE_URL` 和 `DEEPSEEK_API_KEY`，并设置 `HTTPS_PROXY`。闸门路由指向 `https://api.deepseek.com/anthropic`。来源：[DeepSeek 模型插件文档](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/llm/llm-deepseek/README.md)、[代理文档](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/network-proxy.md)。 |

实测发现两点：

- Codex 只设置 `OPENAI_BASE_URL` 时，仍向 `api.openai.com` 发请求。因此 Codex 必须使用配置文件。
- 使用配置文件后，Codex 仍会访问 `chatgpt.com`。闸门按安全档位处理这个请求。

DeepSeek Harness 的接入方式已经实测，见 [security.md](../security.md#6-验证记录) 的 V-13。

## 备选方案

| 方案 | 不选的原因 |
|---|---|
| 闸门解密 HTTPS，在任意请求中替换占位符 | 沙箱要信任额外的根证书。闸门能看到全部请求内容，隐私风险更大。实现更复杂。 |
| 把真实密钥放进沙箱的环境变量 | 违反安全红线"密钥不进舱"。 |
| 闸门运行在容器内 | 违反安全红线"密钥不落地"：真实密钥要离开宿主机进程。 |
| 沙箱使用普通 bridge 网络，靠代理设置限制出网 | agent 可以忽略代理设置直接出网。 |

## 后果

- 好处：真实密钥只发往预设的模型接口。沙箱中不需要额外的根证书。实测证明出网只能经过闸门，见 [security.md](../security.md#6-验证记录)。
- 坏处：只有支持自定义接口地址的 agent 才能使用占位符。闸门不检查白名单主机的请求内容。
- 每个 agent 的接入方式写在 `presets/` 下的声明文件中。
- DeepSeek Harness 通过网页界面交互。入站链路见 [ADR-005](005-inbound-web-ui.md)。
