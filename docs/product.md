# 产品设计

## 1. 产品简介

Airlock（气闸舱）是一个面向桌面用户的 AI agent 安全沙箱。用户在自己的电脑上运行 Claude Code、Codex 等 agent。Airlock 防止三类问题：

- agent 拿到真实密钥后，把密钥发给攻击者。
- agent 访问用户不知道的网站。
- agent 在项目里留下后门文件。

Airlock 的设计目标是：用户不懂安全，也能看懂发生了什么，并做出正确决定。用户做错决定时，损失也有上限。

### 1.1 工作方式

Airlock 在 agent 工作的三个环节做检查：

| 环节 | Airlock 做什么 | 设计记录 |
|---|---|---|
| 进舱 | 检查项目文件，不把密钥文件放进沙箱。 | — |
| 舱内 | 在隔离的沙箱中运行 agent。沙箱中只有占位符。出网请求都经过闸门。 | [ADR-001](adr/001-isolation-backend.md)、[ADR-002](adr/002-egress-and-secret-injection.md)、[ADR-005](adr/005-inbound-web-ui.md) |
| 出舱 | agent 的改动先写入暂存区。用户在运行中就能看到和撤销改动。运行停止、用户审查后，改动才写入项目。 | [ADR-003](adr/003-exit-review-staging.md)、[ADR-007](adr/007-review-during-run.md) |

沙箱中没有真实密钥。所以用户误允许一个主机时，对方只能拿到占位符。

## 2. 术语

全部文档统一使用下列术语。

| 术语 | 含义 |
|---|---|
| agent | 能自己执行命令、读写文件、访问网络的 AI 程序。例如 Claude Code、Codex。 |
| 沙箱 | 运行 agent 的隔离环境。agent 只能看到沙箱内的文件和网络。 |
| 工作舱 | 用户创建的一组设置：一个项目文件夹、一个 agent、一个安全档位。启动工作舱时，Airlock 创建一个沙箱。 |
| 运行 | 工作舱从启动到停止的过程。一次运行中，agent 自己可以开启多个对话。 |
| 闸门 | 运行在宿主机上的网络关口。沙箱的全部出网请求和响应都经过闸门。闸门按安全档位自动放行，暂扣档位以外的请求，并执行用户的决定。闸门替换占位符，是唯一接触真实密钥的部分。 |
| 中继容器 | 在 internal 网络和普通网络之间转发流量的容器。出网中继把沙箱的请求转发给闸门。入站中继把浏览器的请求转发给沙箱内的网页服务。中继容器不持有密钥。 |
| 密钥 | API key、token 等可以代表用户身份的字符串。 |
| 系统钥匙串 | 操作系统保存密钥的位置。在 Windows 上是 Windows 凭据管理器。 |
| 占位符 | 沙箱内代替真实密钥的随机字符串。占位符本身没有权限。 |
| 安全档位 | 一组预设的出网规则。档位分为严格、标准、宽松。 |
| 暂扣 | 闸门收到档位以外的请求时，先挂起请求，等用户决定。 |
| 拦截卡片 | 控制台上显示暂扣请求的卡片。 |
| 进舱检查 | 启动沙箱前，对项目文件做的检查。 |
| 出舱审查 | agent 的改动写入项目前，由用户做的检查。 |
| 暂存区 | 存放 agent 改动的位置。用户确认前，改动不写入项目文件夹。 |
| 运行报告 | 一次运行结束后生成的总结。 |
| 安全副驾 | 用大白话解释风险的 AI 功能。安全副驾只做解释和建议，不做放行决定。闸门不依赖安全副驾。 |
| 控制台 | Airlock 在浏览器中打开的本地管理页面。 |

## 3. 目标用户

### 3.1 用户画像

Airlock 的用户是在个人电脑上运行 agent 的人：

| 用户 | 特征 | 主要顾虑 |
|---|---|---|
| 开发者 | 使用 Claude Code、Codex、DeepSeek Harness 写代码。熟悉命令行，但不熟悉容器和网络安全。 | 想让 agent 自动工作，又怕 agent 误删文件或泄露密钥。 |
| 进阶用户 | 使用 agent 处理日常任务。不写代码，也不读配置文件。 | 看不懂 agent 在做什么，出了问题不知道怎么办。 |

用户需要一台 Windows 电脑，并已安装 Docker Desktop。

### 3.2 需求依据

下列数据来自 [Stack Overflow 2025 开发者调查](https://survey.stackoverflow.co/2025/ai)：

- 84% 的受访者已经使用或计划使用 AI 工具。
- 31% 的受访者已经在工作中使用 AI agent。其中 14.1% 每天使用，9% 每周使用，7.8% 每月使用或偶尔使用。另有 17.4% 计划使用。
- 81.4% 的受访者担心 AI agent 带来的数据隐私和安全问题。

agent 已经出现针对普通用户的攻击：

- ClawHub 是 OpenClaw 的 skill 市场。Koi Security 审计了其中的 2,857 个 skill，发现 341 个是恶意 skill。其中 335 个会诱导用户安装窃密木马 Atomic Stealer。来源：[The Hacker News](https://thehackernews.com/2026/02/researchers-find-341-malicious-clawhub.html)、[SC Media](https://www.scworld.com/news/openclaw-agents-targeted-with-341-malicious-clawhub-skills)。
- 这些恶意 skill 伪装成加密钱包工具、YouTube 工具等常见工具。用户无法逐行检查 agent 读取的每个文件。

## 4. 使用场景

| 编号 | 场景 | Airlock 的处理 |
|---|---|---|
| S-1 | 用户让 agent 修改一个从网上下载的项目。项目的 README 里藏有注入指令，要求 agent 把密钥发到陌生网站。 | 沙箱里只有占位符。闸门暂扣发往陌生网站的请求。拦截卡片说明风险。 |
| S-2 | 项目文件夹里有 `.env` 文件，文件中有数据库密码。 | 进舱检查发现 `.env` 文件，不把它放进沙箱。 |
| S-3 | agent 修改了 `.git/hooks/pre-commit`。用户下次在宿主机上提交代码时，这个文件会自动运行。 | 出舱审查把这个文件标为高风险，并解释后果。 |
| S-4 | agent 需要安装依赖包。 | 标准档位放行常用包管理源，用户不需要逐个确认。 |
| S-5 | 用户想用国产模型。 | 用户选择 DeepSeek Harness 预设。密钥同样不进沙箱。 |
| S-6 | 一次运行正常结束，没有出现风险。 | 运行报告列出 agent 访问的网站、使用密钥的次数、改动的文件。 |

## 5. 产品功能

F-1 到 F-9 按用户的使用顺序排列。F-10 和 F-11 贯穿全部步骤。

### F-1 安装与首次启动

- 用户双击 `airlock.exe`。Airlock 在浏览器中打开控制台。
- 首次启动时，向导检查 Docker Desktop 是否已安装、是否正在运行。检查不通过时，向导告诉用户下一步操作。
- 用户首次启用安全副驾时，控制台说明发送的数据和模型供应商。用户可以关闭安全副驾。安全副驾见 F-11。

### F-2 密钥管理

- 用户选择模型供应商，粘贴密钥，点击"测试连接"。
- Airlock 把密钥存入系统钥匙串。控制台只显示密钥的后 4 位。
- 用户可以删除密钥。

### F-3 工作舱管理

- 用户选择项目文件夹、agent 和安全档位，创建工作舱。
- 控制台列出全部工作舱和它们的状态。
- 用户可以启动和停止工作舱。停止工作舱时，本次运行结束，Airlock 进入出舱审查。
- 用户可以重置工作舱。重置会丢弃沙箱和暂存区中的全部改动。

### F-4 安全档位

| 档位 | 自动放行的出网目标 |
|---|---|
| 严格 | 只有模型接口。 |
| 标准 | 模型接口和常用包管理源。 |
| 宽松 | 模型接口、常用包管理源和常用开发网站。 |

- 新建的工作舱默认使用标准档位。默认放行的理由见 [security.md](security.md#7-已知限制)。
- 档位以外的请求一律暂扣。用户不需要手写规则。

### F-5 进舱检查

- 启动沙箱前，Airlock 检查项目文件夹。
- `.env` 文件和私钥文件不放进沙箱。
- 疑似注入指令的文件标为"需要注意"。
- 控制台用一张卡片列出检查结果。

### F-6 舱内运行

- 控制台的运行页面分为两部分：左侧是 agent 的界面，右侧是出网动态和拦截卡片。用户在一个页面中和 agent 对话，同时看到 agent 的出网情况。
- Claude Code 和 Codex 的终端界面嵌入运行页面。
- DeepSeek Harness 的网页界面嵌入运行页面。
- 出网动态实时列出 agent 访问的网站和暂扣次数。
- 右侧还实时列出暂存区中的改动。高风险改动标为红色，并附解释。
- 用户可以撤销某个改动。这个文件恢复到本次运行开始时的状态，agent 继续工作。用户可以在 agent 界面中告诉 agent 换一种做法。
- 关闭或刷新运行页面不会结束 agent。用户回到运行页面时，继续原来的对话。
- 用户也可以在独立的终端窗口中使用 Claude Code 和 Codex。终端窗口连接的是同一个 agent。

### F-7 拦截卡片

每张拦截卡片回答三个问题：

1. 发生了什么。
2. 风险有多大。
3. 用户可以怎么做。

- 用户可以点击"本次运行允许"或"拒绝"。"本次运行允许"只对本次运行中的这个主机有效。运行结束后，允许失效。
- 用户在限定时间内没有决定时，闸门拒绝这个请求。
- 卡片先显示规则模板生成的解释，用户可以立即决定。安全副驾的解释生成后，替换模板解释。
- 安全副驾不可用时，卡片保留模板解释，并标明"AI 解释暂不可用"。

### F-8 出舱审查

- 运行停止后，控制台按文件列出暂存区中的改动。
- 高风险改动标为红色，并附解释。高风险改动包括 Git hooks、CI 配置、依赖安装脚本、新增的可执行文件。
- 默认把全部改动应用到项目。用户也可以丢弃某个文件。这时控制台提示：改动可能不完整，项目可能无法运行。
- 改动只在运行停止后写入项目。运行中 agent 还会修改文件，用户在运行中看到的改动可能不是最终结果。
- 用户想在运行中拒绝某个改动时，用 F-6 的撤销功能。这样 agent 还可以接着修改，改动保持完整。

### F-9 运行报告

出舱审查完成后，控制台生成运行报告。报告用大白话说明下列内容：

- agent 访问了哪些网站。
- 哪些请求被暂扣，用户做了什么决定。
- 密钥被使用了几次，用于哪个模型供应商。
- agent 改动了哪些文件，用户应用了哪些改动。

### F-10 agent 预设

| 预设 | agent | 模型供应商 |
|---|---|---|
| Claude Code | Claude Code | Anthropic |
| Codex | Codex | OpenAI |
| DeepSeek Harness | DeepSeek Harness（`dsh`） | DeepSeek |

DeepSeek Harness 是 DeepSeek 开源的 agent，使用 MIT 许可证。来源：[官方仓库](https://github.com/deepseek-ai/deepseek-harness)。

### F-11 安全副驾

- 安全副驾使用 Anthropic 或 DeepSeek 的模型。两家都提供 Anthropic 格式的接口。来源：[DeepSeek 接口文档](https://api-docs.deepseek.com/guides/anthropic_api)。
- 用户只添加了其中一家的密钥时，安全副驾使用这一家。用户添加了两家的密钥时，在设置中选择一家。
- 用户没有这两家的密钥时，安全副驾关闭。拦截卡片使用规则模板生成解释。

### 5.1 范围

产品不包含下列内容：

- macOS 和 Linux 支持。
- MCP 工具调用管理。沙箱中不提供 MCP 配置。
- 团队和组织的集中管理。
- 云端沙箱。
- 解密 HTTPS 流量。

## 6. 竞品分析

### 6.1 竞品对比

| 产品 | 隔离方式 | 密钥不进沙箱 | 出网控制 | 界面 | 来源 |
|---|---|---|---|---|---|
| Docker Sandboxes | 每个沙箱一个 microVM | 是 | 默认拒绝。用户用命令批准，批准只对之后的请求生效 | 命令行和终端面板 | [安全](https://docs.docker.com/ai/sandboxes/security)、[凭据](https://docs.docker.com/ai/sandboxes/configuration/credentials/)、[网络策略](https://docs.docker.com/ai/sandboxes/security/policy) |
| NVIDIA OpenShell | 沙箱运行时 | 未查到官方说明 | 按策略拦截。用户在终端界面中批准或拒绝 | 终端界面 | [NemoClaw 文档](https://docs.nvidia.com/nemoclaw/user-guide/openclaw/network-policy/approve-network-requests.md) |
| Claude Code 内置沙箱 | 操作系统进程沙箱，不支持原生 Windows | 可选的占位符模式 | 按域名白名单。首次访问新域名时提示 | 终端 | [沙箱文档](https://code.claude.com/docs/en/sandboxing) |
| Codex 内置沙箱 | 操作系统进程沙箱 | 否 | 默认断网 | 终端 | [安全文档](https://developers.openai.com/codex/security) |
| Windows Sandbox | 一次性虚拟机，不支持家庭版 | 否 | 默认联网 | 图形桌面 | [文档](https://learn.microsoft.com/en-us/windows/security/application-security/application-isolation/windows-sandbox/windows-sandbox-overview) |
| 腾讯云密钥沙箱 | 云端服务 | 是 | 不适用 | 云服务 | [IT之家报道（凤凰网转载）](https://tech.ifeng.com/c/8rajF3DzqGJ) |

### 6.2 竞品缺口与 Airlock 的差异

| 竞品缺口 | Airlock 的做法 |
|---|---|
| 表中的产品都不解释拦截的风险。它们最多显示被拦的主机、端口和请求路径。 | 拦截卡片用大白话回答三个问题（F-7）。 |
| 面向 agent 的产品都使用命令行或终端界面。 | 图形控制台，常规操作不需要命令行（F-1 到 F-9）。 |
| Docker Sandboxes 的直接模式下，agent 对 Git hooks、CI 配置和构建文件的改动立即出现在宿主机上，需要用户自己检查。来源：[安全文档](https://docs.docker.com/ai/sandboxes/security)。 | 改动先写入暂存区，经过出舱审查才写入项目（F-8）。 |
| Docker Sandboxes 内置的模型供应商中没有国产模型供应商。来源：[凭据文档](https://docs.docker.com/ai/sandboxes/configuration/credentials/)。 | 提供 DeepSeek Harness 预设（F-10）。安全副驾支持 DeepSeek 模型（F-11）。 |

## 7. 商业分析

### 7.1 商业模式

- 个人版免费，源代码使用 Apache-2.0 许可证开放。
- 团队版收费（估计）。团队版向成员统一下发安全档位，并汇总运行报告。依据：Docker Sandboxes 的命令行工具和本地沙箱免费，组织级的集中策略管理需要单独付费订阅。来源：[Docker Sandboxes 文档](https://docs.docker.com/ai/sandboxes/)。

### 7.2 成本

- 沙箱运行在用户自己的电脑上。Airlock 没有服务器成本。
- 安全副驾使用用户自己的模型密钥。每次运行有调用次数上限，用户的额外费用可控（估计，依据：每次解释只发送事件元数据，请求很小）。

### 7.3 风险

| 风险 | 应对 |
|---|---|
| Docker、NVIDIA 等大厂补上"看得懂"的功能。 | 发挥中文界面和国产模型预设的优势，尽快积累用户。 |
| 用户需要先安装 Docker Desktop，上手门槛较高。 | 首次启动向导检查环境，并给出下一步操作。 |
