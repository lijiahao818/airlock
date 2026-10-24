# 架构设计

本文档说明 Airlock 的模块划分、模块规格和分工。产品术语见 [product.md](product.md#2-术语)。安全设计见 [security.md](security.md)。

## 1. 设计方法

本文档按下列原则分解系统：

- 架构是业务的正交分解。每个模块只负责一个业务。
- 系统分为一个最小的核心和多个扩展。扩展之间不直接调用，只通过核心建立联系。
- 模块的接口是共识的载体。接口用 Go 代码表达，图只做辅助。
- 模块的使用界面要符合 Go 的惯例。模块的环境依赖要最少。
- 只在三种情况下把依赖抽象为接口：有多种实现、要替换重依赖做测试、依赖是可选组件。

## 2. 命名

代码中的名称与产品术语一一对应。一个名称只表示一个概念。

| 产品术语 | 代码中的名称 | 说明 |
|---|---|---|
| 工作舱 | `cabin.Cabin` | 不用 workspace，避免与沙箱内的 `/workspace` 目录混淆。 |
| 运行 | run | 一次运行有一个 run ID。 |
| 闸门 | `gate` | |
| 暂扣 | `Hold` | |
| 安全档位 | `Tier` | 取值为 `strict`、`standard`、`permissive`。不用 level，避免与风险等级混淆。 |
| 风险等级 | `RiskLevel` | 取值为 `low`、`medium`、`high`。 |
| 进舱检查、出舱审查的文件规则 | `filecheck` | |
| 暂存区 | `staging.Area` | |
| 安全副驾 | `advisor` | 不用 copilot，避免与 GitHub Copilot 混淆。advisor 也表明它只提建议。 |
| 运行报告 | `audit.Report` | |
| 控制台 | `console` | |
| 网页终端 | `webterm` | 嵌入运行页面的终端界面，见 [ADR-006](adr/006-embed-agent-ui.md)。 |
| agent 预设 | `presets.Preset` | |
| agent 界面的类型 | `Preset.UI` | 取值为 `terminal`、`web`。 |

`internal/vault` 和 `presets/` 这两个名称由 [CLAUDE.md](../CLAUDE.md) 规定，本文档沿用。

## 3. 需求分析

### 3.1 稳定点、变化点和不会发生的需求

| 类别 | 内容 | 处理方式 |
|---|---|---|
| 稳定点 | 一次运行的生命周期：进舱、舱内、出舱。 | 核心模块 `cabin`。 |
| 稳定点 | 出网判定和密钥替换。 | 核心模块 `gate`。 |
| 稳定点 | 改动先进暂存区，用户确认后才写入项目。 | 核心模块 `staging`。 |
| 变化点 | agent 的种类。 | `presets/` 下的声明文件。新增 agent 不改代码。 |
| 变化点 | 隔离后端。 | `sandbox.Backend` 接口。 |
| 变化点 | 安全档位中的主机列表。 | `gate` 中的一张数据表。 |
| 变化点 | 模型供应商。 | `gate` 中的一张路由表。 |
| 变化点 | 进舱检查和出舱审查的文件规则。 | 扩展 `filecheck`，注册到核心。 |
| 变化点 | AI 解释的生成方式。 | 扩展 `advisor`，注册到核心。 |
| 变化点 | 界面。 | `console`、`webterm` 和前端 `web/`。 |
| 不会发生 | macOS 和 Linux 宿主机、MCP 管理、团队管理、云端沙箱、解密 HTTPS。 | 见 [product.md](product.md#51-范围)。不设计扩展点。 |
| 不会发生 | 跨运行保存的允许规则。 | "本次运行允许"随闸门实例一起失效。 |
| 不会发生 | 更换存储方式。 | 状态用 JSON 文件，审计记录用 JSONL 文件。不抽象存储接口。 |

### 3.2 分层

核心是产品的最小功能集：在沙箱中运行一个 agent，出网经过闸门，改动经过暂存区。

| 层 | 模块 | 可以导入 |
|---|---|---|
| 入口层 | `cmd/airlock` | 全部模块。只做组装。 |
| 界面层 | `console`、`webterm` | 核心层和扩展层的公开接口。 |
| 扩展层 | `advisor`、`filecheck` | 核心层的公开接口。 |
| 核心层 | `cabin`、`gate`、`sandbox`、`staging`、`vault`、`audit`、`presets`、`desktop` | 见 4.2 节。 |

产品不管理 MCP（见 [product.md](product.md#51-范围)），所以不建立 `internal/mcphub`。

## 4. 模块

### 4.1 模块职责

每个模块写明它做什么，也写明它不做什么。

| 模块 | 目录 | 做什么 | 不做什么 |
|---|---|---|---|
| cabin | `internal/cabin` | 管理工作舱和运行的生命周期。把暂扣请求转给用户。用规则模板生成暂扣解释。 | 不做出网判定。不读写项目文件的内容。不调用模型。 |
| gate | `internal/gate` | 监听出网请求。按档位和用户决定放行或拒绝。在模型路由上把占位符换成真实密钥。 | 不知道工作舱、界面和安全副驾。不保存密钥。不记录请求体、请求头和查询参数。 |
| sandbox | `internal/sandbox` | 创建和删除沙箱、中继容器和 internal 网络。构建镜像。在沙箱中打开终端会话。 | 不知道 agent 的种类。不处理密钥。 |
| staging | `internal/staging` | 把项目复制到暂存区。列出改动。撤销单个改动。把单个改动安全地写回项目。 | 不判断改动的风险。不决定哪些文件不复制。 |
| vault | `internal/vault` | 在系统钥匙串中保存、读取、删除密钥。 | 不验证密钥是否可用。不把密钥写入文件。 |
| audit | `internal/audit` | 记录一次运行的出网判定和出舱审查决定。生成运行报告。 | 不做判定。不记录请求内容。 |
| presets | `presets/` | 用声明文件描述每个 agent：镜像、命令、环境变量、配置文件、模型供应商、界面类型。 | 不包含真实密钥。 |
| desktop | `internal/desktop` | 打开浏览器、打开终端窗口、弹出文件夹选择框。 | 不包含业务逻辑。 |
| filecheck | `internal/filecheck` | 进舱检查：找出不能复制的文件和需要注意的文件。出舱审查：标出高风险改动。 | 不复制文件。不写回项目。 |
| advisor | `internal/advisor` | 调用模型，为暂扣请求生成 AI 解释。保存安全副驾的设置。 | 不做放行决定。不读取请求内容。不生成规则模板解释。 |
| console | `internal/console`、`web/` | 提供控制台的 HTTP API 和网页。校验令牌和 Host 头。 | 不包含业务规则。不显示 agent 的输出。 |
| webterm | `internal/webterm`、`web/` | 在独立的端口上提供网页终端。把 WebSocket 转接到沙箱的终端会话。 | 不提供放行功能。不渲染链接和 HTML。 |

### 4.2 依赖图

```
                         cmd/airlock（组装全部模块）

 界面层          console                         webterm
                    │ 调用                          │ 调用
                    ▼                               ▼
 核心层   ┌───────────────────────── cabin ─────────────────────────┐
          │ 调用 gate、sandbox、staging、audit、presets、desktop      │   vault
          └──────────────────────────────────────────────────────────┘
                    ▲ 注册                          ▲ 注册
 扩展层          advisor                         filecheck
```

`vault` 不被 `cabin` 导入。`cmd/airlock` 把 `vault.Get` 传给 `cabin.Options.Secret`，并用 `vault` 实现 `console.Keys`。

模块之间有三类关系：

| 关系 | 例子 |
|---|---|
| 调用公开接口 | `console` 调用 `cabin.Service`。`cabin` 调用 `gate`、`sandbox`、`staging`、`audit`、`presets`、`desktop`。 |
| 事件 | `gate` 通过 `Config.Ask` 和 `Config.OnEvent` 通知 `cabin`。`console` 通过 `cabin.Service.Subscribe` 接收事件。 |
| 注册 | `advisor` 实现 `cabin.Explainer`，`filecheck` 实现 `cabin.Inspector`。`cmd/airlock` 把它们传给 `cabin.New`。 |

依赖规则：

- 下层不导入上层。核心层只通过 `cabin.Explainer` 和 `cabin.Inspector` 使用扩展。
- `advisor` 和 `filecheck` 互不导入。`console` 和 `webterm` 互不导入。
- `gate`、`sandbox`、`staging`、`vault`、`presets`、`desktop` 只使用标准库，彼此不导入。
- `audit` 只导入 `gate` 的事件类型。`cabin` 不导入 `vault`，真实密钥通过 `cabin.Options.Secret` 传入。

### 4.3 依赖图保证安全红线

- 红线 4（AI 不能单独放行）：`gate` 的放行依据只有 `Config.Allow` 和 `Config.Ask` 的返回值。`cabin` 只在 `Service.Decide` 被调用时让 `Ask` 返回允许。`Service.Decide` 只由控制台的用户点击触发。
- 红线 4：`advisor` 的输出只写入 `Hold.Explanation`。`cabin` 不读取解释内容做判定。`gate` 不导入 `advisor`。
- 红线 1 和红线 2（密钥不进舱、不落地）：真实密钥只出现在 `vault`、`gate` 和控制台保存密钥的请求处理中。`sandbox.Spec` 中只有占位符。
- 红线 6（不信任沙箱输入）：`staging`、`filecheck`、`webterm` 和 `cabin` 的令牌提取处理来自沙箱的数据。它们的单元测试必须覆盖拒绝路径。

## 5. 模块规格

本节的 Go 代码是模块之间的约定。实现时，导出的名称和签名必须与本节一致。修改导出接口时，先修改本节。

各包中已有对应的契约代码。通过 HTTP 传输的类型带有 `json` 标签，字段的 JSON 名称以代码为准。

### 5.1 gate

```go
package gate

// Event 是一次出网判定的元数据。不含请求体、请求头和查询参数。
type Event struct {
	Time     time.Time
	Host     string
	Port     int
	Method   string // CONNECT，或模型路由上的 HTTP 方法
	Path     string // 只有模型路由有路径，不含查询参数
	Rule     string // 命中的规则，取值见下表
	Allowed  bool
	Provider string // 换上了真实密钥时为供应商名，否则为空
}

type Config struct {
	// Host 是非 CONNECT 请求必须携带的 Host 头。为空时要求 127.0.0.1:<闸门端口>。
	Host      string
	Allow     []string // 自动放行的主机，来自安全档位
	Providers []string // 开放的模型路由
	// Secret 返回供应商的真实密钥。闸门不缓存返回值。
	Secret func(provider string) (string, error)
	// Ask 在请求被暂扣时调用。返回 true 表示用户允许。
	// ctx 在 HoldTimeout 后取消，这时调用方必须返回 false。
	Ask     func(ctx context.Context, e Event) bool
	OnEvent func(e Event)
}

const HoldTimeout = 60 * time.Second

type Gate struct{ /* 未导出 */ }

func Start(cfg Config) (*Gate, error) // 监听 127.0.0.1 上的随机端口
func (g *Gate) Port() int
func (g *Gate) Placeholder(provider string) string // 本实例签发的占位符
func (g *Gate) Close() error

func Providers() []string                       // anthropic、openai、deepseek
func Tiers() []string                           // strict、standard、permissive
func TierHosts(tier string) ([]string, error)   // 档位自动放行的主机
func TestKey(ctx context.Context, provider, secret string) error // 用密钥调用供应商的接口
```

模型路由：

| 供应商 | 闸门路由 | 上游地址 |
|---|---|---|
| anthropic | `/anthropic` | `https://api.anthropic.com` |
| openai | `/openai` | `https://api.openai.com` |
| deepseek | `/deepseek` | `https://api.deepseek.com/anthropic` |

判定规则（`Event.Rule` 的取值）：

| Rule | 条件 | 结果 |
|---|---|---|
| `route` | 请求发往开放的模型路由，并携带本实例签发的占位符。 | 放行，换上真实密钥。 |
| `bad-placeholder` | 请求发往模型路由，但占位符不对。 | 拒绝。 |
| `bad-host` | 非 CONNECT 请求的 Host 头不等于 `Config.Host`。 | 拒绝。 |
| `unsupported` | 既不是 CONNECT，也不是模型路由。 | 拒绝。 |
| `tier` | CONNECT 的目标主机在 `Config.Allow` 中，端口为 443。 | 放行。 |
| `run-allow` | 用户在本次运行中允许过这个主机。 | 放行。 |
| `run-deny` | 用户在本次运行中拒绝过这个主机。 | 拒绝，不再询问。 |
| `user-allow` | 暂扣后用户允许。 | 放行。以后记为 `run-allow`。 |
| `user-deny` | 暂扣后用户拒绝。 | 拒绝。以后记为 `run-deny`。 |
| `timeout` | 暂扣后 `HoldTimeout` 内没有决定。 | 拒绝。下次请求再次询问。 |

- 暂扣时，传给 `Ask` 的事件中 `Rule` 为 `hold`，`Allowed` 为 false。
- 同一个主机的并发暂扣只调用一次 `Ask`。
- 一个闸门实例对应一次运行。实例关闭后，`run-allow` 和 `run-deny` 一起失效。
- 路由表的上游地址不导出。单元测试在包内把上游替换为本地 mock。

### 5.2 sandbox

```go
package sandbox

type Spec struct {
	Name       string            // 只含小写字母、数字和连字符
	Image      string
	Dockerfile []byte            // 镜像不存在时用它构建
	StagingDir string            // 宿主机上的暂存区目录，挂载到 /workspace
	Env        map[string]string // 只含占位符和地址，不含真实密钥
	Files      map[string]string // 沙箱内的路径 → 文件内容，agent 启动前写入
	Cmd        []string          // 容器的主进程。为空时运行空闲进程
	GatePort   int               // 闸门在宿主机上的端口，出网中继转发到这里
	WebPort    int               // 沙箱内网页服务的端口。0 表示没有网页界面
	HostPort   int               // 入站中继在宿主机 127.0.0.1 上的端口
}

// Session 是到沙箱内 agent 会话的一个终端连接。
type Session interface {
	io.ReadWriteCloser // 读：终端输出。写：键盘输入。Close：断开连接，agent 继续运行
	Resize(cols, rows int) error
}

type Backend interface {
	Check(ctx context.Context) error                                  // Docker Desktop 是否安装、是否运行
	Create(ctx context.Context, s Spec) (id string, err error)         // 创建网络、中继容器和沙箱，并启动
	Exec(ctx context.Context, id string, cmd []string) (Session, error) // 连接运行 cmd 的 agent 会话。会话不存在时启动 cmd
	AttachCommand(id string, cmd []string) []string                    // 在宿主机终端窗口中连接同一个 agent 会话的命令行
	Logs(ctx context.Context, id string) (io.ReadCloser, error)       // 沙箱主进程的输出
	Remove(ctx context.Context, id string) error                       // 删除沙箱、中继容器和网络
	Prune(ctx context.Context) error                                   // 删除 Airlock 创建的全部残留资源
}

func Docker() Backend // 调用 docker 命令行实现，不引入 Docker SDK
```

- `Create` 按 [ADR-001](adr/001-isolation-backend.md) 的参数启动沙箱。
- 沙箱只接入 internal 网络。出网中继在 internal 网络上的别名为 `airlock-gate`，监听 8080 端口。
- `WebPort` 不为 0 时，`Create` 启动入站中继，把 `127.0.0.1:<HostPort>` 映射到沙箱的 `WebPort`。
- `Create` 给全部资源加上标签 `airlock=1`。`Prune` 按这个标签删除资源。
- terminal 型的 agent 运行在沙箱内的 tmux 会话中。`Exec` 和 `AttachCommand` 连接同一个会话，所以网页终端和独立终端窗口看到的是同一个 agent。断开连接不结束 agent。删除沙箱时 agent 结束。terminal 型预设的镜像必须安装 tmux。
- `Exec` 的宿主机一侧没有控制台。`docker exec -t` 在这种情况下会失败。实现前先实测可行的方式，例如在沙箱内用 `script` 分配终端，或在宿主机上用 ConPTY。

### 5.3 staging

```go
package staging

type Kind string

const (
	Added    Kind = "added"
	Modified Kind = "modified"
	Deleted  Kind = "deleted"
)

type Change struct {
	Path     string // 斜杠分隔的相对路径
	Kind     Kind
	Conflict bool   // 运行期间，项目文件夹中的同一文件也有改动
	Special  bool   // 符号链接或特殊文件，不能写回项目
}

type Area struct{ /* 未导出 */ }

// Create 在 dir 下建立暂存区，把 project 复制进去。skip 返回 true 的文件不复制。
func Create(project, dir string, skip func(rel string) bool) (*Area, error)
func Open(project, dir string) (*Area, error) // 打开已有的暂存区
func (a *Area) Files() string                 // 挂载进沙箱的目录
func (a *Area) Changes() ([]Change, error)
func (a *Area) Revert(rel string) error       // 撤销一个改动，恢复到基线
func (a *Area) Apply(rel string) error        // 把一个改动写回项目
func (a *Area) Discard() error                // 删除整个暂存区
```

复制和比较的方法（[ADR-003](adr/003-exit-review-staging.md) 要求本文档确定）：

1. `Create` 把项目文件复制到 `dir/files`，包括 `.git` 目录。
2. `Create` 把每个复制的文件的 SHA-256 写入 `dir/manifest.json`。这个文件是基线清单，不挂载进沙箱。
3. `Changes` 遍历 `dir/files`，与基线清单比较，得出新增、修改和删除的文件。
4. `Changes` 计算项目文件夹中对应文件的 SHA-256。结果与基线不同时，标记 `Conflict`。暂存区新增的文件在项目中已经存在时，也标记 `Conflict`。
5. `Apply` 拒绝 `Conflict` 和 `Special` 的改动。`Apply` 拒绝越出项目文件夹的路径。`Apply` 不跟随符号链接。
6. `Revert`（[ADR-007](adr/007-review-during-run.md)）只从基线清单中的文件恢复：把项目中的同名文件复制回暂存区。不在基线清单中的文件，`Revert` 从暂存区删除。所以进舱检查排除的文件不会被复制进暂存区。
7. `Revert` 拒绝 `Conflict` 的改动，因为项目中的文件已经不是基线。`Revert` 拒绝越出暂存区的路径，不跟随符号链接。

单元测试必须覆盖下列拒绝路径：符号链接、`..` 路径、冲突、特殊文件、撤销不在基线清单中的文件。

### 5.4 vault

```go
package vault

var ErrNotFound = errors.New("vault: 没有找到密钥")

func Set(provider, secret string) error
func Get(provider string) (string, error)
func Delete(provider string) error
func Hint(provider string) (string, error) // 密钥的后 4 位
```

- Windows 上用标准库 `syscall` 调用凭据管理器的 `CredWriteW`、`CredReadW`、`CredDeleteW`。条目名为 `airlock:<provider>`。
- 其他系统上，全部函数返回错误。

### 5.5 audit

```go
package audit

type Entry struct {
	Gate   *gate.Event // 一次出网判定
	Review *Review     // 一个出舱审查决定
}

type Review struct {
	Time    time.Time
	Path    string
	Applied bool
}

type Log struct{ /* 未导出 */ }

func Open(path string) (*Log, error) // 打开 JSONL 文件，在末尾追加
func (l *Log) Add(e Entry) error
func (l *Log) Close() error
func Read(path string) ([]Entry, error)

type Report struct {
	Hosts   []HostSummary  // agent 访问过的主机
	Holds   []HoldSummary  // 暂扣过的主机和用户的决定
	KeyUses map[string]int // 供应商 → 换上真实密钥的次数
	Files   []Review       // 改动的文件和用户的决定
}

type HostSummary struct {
	Host            string
	Allowed, Denied int
}

type HoldSummary struct {
	Host string
	Rule string // user-allow、user-deny 或 timeout
}

func Summarize(entries []Entry) Report
```

### 5.6 presets

每个 agent 是 `presets/` 下的一个目录，包含 `preset.json` 和 `Dockerfile`。`presets` 包用 `embed` 把它们打包进程序。

目录名就是预设 ID，与 [product.md](product.md#f-10-agent-预设) 的 F-10 对应：

| 预设 ID | agent |
|---|---|
| `claude-code` | Claude Code |
| `codex` | Codex |
| `deepseek-harness` | DeepSeek Harness |

```go
package presets

type Preset struct {
	ID         string
	Name       string
	Provider   string            // gate.Providers() 中的一个
	UI         string            // terminal 或 web
	Cmd        []string          // terminal：在终端中运行的命令。web：容器的主进程
	Env        map[string]string // 值中可以使用模板变量
	Files      map[string]string // 沙箱内的路径 → 内容，可以使用模板变量
	Web        *Web              // UI 为 web 时必填
	Dockerfile []byte
}

type Web struct {
	Port         int    // 沙箱内网页服务的端口
	TokenPattern string // 从启动输出中提取令牌的正则表达式，必须有一个分组
	OpenPath     string // 网页界面的路径，例如 /?token={token}
}

func All() ([]Preset, error)
func Get(id string) (Preset, error)
```

模板变量：

| 变量 | 值 |
|---|---|
| `{gate}` | `http://airlock-gate:8080` |
| `{placeholder}` | 闸门为这个供应商签发的占位符 |
| `{webhost}` | `127.0.0.1:<HostPort>` |
| `{token}` | 从启动输出中提取的令牌，只用于 `OpenPath` |

Claude Code 预设示例：

```json
{
  "id": "claude-code",
  "name": "Claude Code",
  "provider": "anthropic",
  "ui": "terminal",
  "cmd": ["claude"],
  "env": {
    "ANTHROPIC_BASE_URL": "{gate}/anthropic",
    "ANTHROPIC_API_KEY": "{placeholder}",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
    "HTTPS_PROXY": "{gate}"
  }
}
```

### 5.7 cabin

```go
package cabin

type Status string

const (
	Idle    Status = "idle"    // 空闲
	Running Status = "running" // 运行中
	Review  Status = "review"  // 等待出舱审查
)

type Cabin struct {
	ID      string
	Name    string // 项目文件夹的名称
	Project string
	Preset  string
	Tier    string
	Status  Status
	RunID   string // 当前或最近一次运行
}

// HoldMeta 是安全副驾可以看到的全部数据。
type HoldMeta struct {
	Agent  string
	Host   string
	Port   int
	Method string
	Path   string
	Rule   string
}

type Explanation struct {
	What          string // 发生了什么
	Risk          string // 风险多大
	RiskLevel     string // low、medium 或 high
	Action        string // 用户可以怎么做
	SuggestAllow  bool   // 只用于显示，不影响判定
	Source        string // template 或 ai
	AIUnavailable bool   // 安全副驾已开启，但这次没有生成 AI 解释
	PromptVersion string // Source 为 ai 时，生成解释的提示词版本
}

type Hold struct {
	ID    string
	Cabin string
	RunID string
	HoldMeta
	Explanation Explanation
}

type FindingKind string

const (
	Excluded FindingKind = "excluded"  // 不复制到暂存区
	Notice   FindingKind = "notice"    // 需要注意
	HighRisk FindingKind = "high-risk" // 高风险改动
)

type Finding struct {
	Path   string
	Kind   FindingKind
	Reason string // 给用户看的大白话
}

type ReviewItem struct {
	staging.Change
	Findings []Finding
}

type Event struct {
	Kind    string // request、hold、explained、hold-closed 或 status
	Cabin   string
	Request *gate.Event // Kind 为 request
	Hold    *Hold       // Kind 为 hold、explained 或 hold-closed
	Status  Status      // Kind 为 status
}

// Service 是界面层使用的全部功能。
type Service interface {
	CheckDocker(ctx context.Context) error

	List() ([]Cabin, error)
	Create(project, preset, tier string) (Cabin, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Reset(ctx context.Context, id string) error

	AgentURL(id string) (string, error)  // 嵌入运行页面的 agent 界面地址
	OpenTerminal(id string) error        // 保底方式：在独立终端窗口中打开 agent
	Attach(ctx context.Context, id, token string) (sandbox.Session, error) // 网页终端使用

	EntryFindings(id string) ([]Finding, error)
	ReviewItems(id string) ([]ReviewItem, error) // Running 和 Review 状态都可以调用
	Revert(id, path string) error               // 只在 Running 状态可用
	Apply(id, path string) error                // 只在 Review 状态可用
	FinishReview(id string) error
	Report(id string) (audit.Report, error) // 最近一次运行的报告

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
	TermURL   string    // 网页终端的地址，例如 http://127.0.0.1:<端口>
	Explainer Explainer // 为 nil 时只用规则模板
	Inspector Inspector // 为 nil 时不排除文件，不标注风险
}

type Manager struct{ /* 未导出 */ }

func New(o Options) (*Manager, error) // *Manager 实现 Service

// Template 用规则模板生成解释。Source 为 template。
func Template(m HoldMeta) Explanation
```

工作舱的状态：

```
Idle ──Start──► Running ──Stop──► Review ──FinishReview──► Idle
  ▲                                  │
  └──────────────Reset───────────────┘   （Running 时 Reset 先执行 Stop）
```

- 只有 `Idle` 状态可以 `Start`。
- `Running` 状态时，`ReviewItems` 返回暂存区当前的改动，供运行页面预览。改动只在 `Review` 状态写入项目。
- `Reset` 丢弃暂存区，不生成运行报告。
- `New` 先调用 `Backend.Prune`，再把 `Running` 状态的工作舱改为 `Review`。
- 规则模板放在 `internal/cabin/template.go`。安全副驾不存在时，拦截卡片仍有解释。
- `AgentURL`：界面为 terminal 时，返回网页终端的地址，见 5.11 节。界面为 web 时，返回 `http://127.0.0.1:<HostPort>` 加上展开后的 `OpenPath`。

### 5.8 advisor

```go
package advisor

type Settings struct {
	Enabled     bool   // 用户开启了安全副驾
	Provider    string // 用户添加了两家的密钥时选择的一家
	NoticeShown bool   // 用户已经看过数据发送说明
}

type Config struct {
	DataDir     string
	BaseURL     string                       // 安全副驾专用闸门的地址
	Placeholder func(provider string) string // 安全副驾专用闸门签发的占位符
	HasKey      func(provider string) bool
}

type Advisor struct{ /* 未导出 */ }

func New(cfg Config) (*Advisor, error) // *Advisor 实现 cabin.Explainer
func (a *Advisor) Settings() Settings
func (a *Advisor) SetSettings(s Settings) error
```

- 模型调用经过一个专用的闸门实例。这个实例只开放 anthropic 和 deepseek 路由，拒绝全部 CONNECT。
- 供应商按 F-11 选择：只有一家的密钥时用这一家。两家都有时用 `Settings.Provider`。两家都没有时返回 `cabin.ErrAdvisorOff`。
- 提示词放在 `internal/advisor/prompt.md`，模型 ID 放在 `internal/advisor/config.json`，都用 `embed` 打包。提示词版本是提示词的 SHA-256 的前 12 位。
- 同一次运行中，同一个主机的解释只生成一次。每次运行最多调用模型 20 次。
- 输出不符合 schema、超过长度上限、超时或调用失败时，返回错误。
- 产品评估集放在 `evals/`，用 `make eval` 运行。

### 5.9 filecheck

```go
package filecheck

type Checker struct{}

func (Checker) Entry(project string) ([]cabin.Finding, error)
func (Checker) Exit(files string, changes []staging.Change) []cabin.Finding
```

- `Entry` 把 `.env` 文件和私钥文件标为 `Excluded`。
- `Entry` 把 agent 会当作指令读取的文件标为 `Notice`，例如 `AGENTS.md`、`CLAUDE.md`、skill 文件。
- `Exit` 把 Git hooks、CI 配置、依赖安装脚本、新增的可执行文件标为 `HighRisk`。
- `Exit` 按文件头判断可执行文件。`Exit` 只读取文件开头的几个字节，并且不跟随符号链接。

### 5.10 console

```go
package console

type Keys interface {
	Hint(provider string) (string, error)
	Set(provider, secret string) error
	Delete(provider string) error
	Test(ctx context.Context, provider string) error
}

type AdvisorSettings interface {
	Settings() advisor.Settings
	SetSettings(s advisor.Settings) error
}

type Deps struct {
	Cabins     cabin.Service
	Keys       Keys            // cmd/airlock 用 vault 和 gate.TestKey 实现
	Advisor    AdvisorSettings
	PickFolder func(ctx context.Context) (string, error)
}

// New 返回控制台的 HTTP 处理器。token 是启动时生成的随机令牌。
func New(d Deps, token string) http.Handler
```

- 每个 `/api` 请求都必须带请求头 `X-Airlock-Token`。
- Host 头只接受 `127.0.0.1:<端口>` 和 `localhost:<端口>`。
- `cmd/airlock` 用 `http://127.0.0.1:<端口>/#token=<令牌>` 打开浏览器。令牌在 URL 片段中，不发给服务器，也不进入服务器日志。
- 前端用 `fetch` 读取事件流，这样事件流也能带令牌请求头。
- 响应中的列表字段没有元素时输出 `[]`，不输出 `null`。
- 运行页面的左侧用 iframe 加载 `AgentURL` 的地址。右侧显示出网动态和拦截卡片。
- 收到 `hold` 事件时立即显示卡片。收到同一 ID 的 `explained` 事件时替换解释。
- 前端构建结果输出到 `internal/console/dist`，用 `//go:embed all:dist` 打包。

HTTP API：

| 请求 | 处理 |
|---|---|
| `get /api/docker` | `Cabins.CheckDocker` |
| `get /api/providers` | `gate.Providers` 和 `Keys.Hint` |
| `put /api/keys/{provider} json {"secret": "..."}` | `Keys.Set` |
| `delete /api/keys/{provider}` | `Keys.Delete` |
| `post /api/keys/{provider}/test` | `Keys.Test` |
| `get /api/presets` | `presets.All` |
| `get /api/tiers` | `gate.Tiers` |
| `get /api/advisor` | `Advisor.Settings` |
| `put /api/advisor json {...}` | `Advisor.SetSettings` |
| `post /api/pick-folder` | `PickFolder` |
| `get /api/cabins` | `Cabins.List` |
| `post /api/cabins json {"project": "...", "preset": "...", "tier": "..."}` | `Cabins.Create` |
| `post /api/cabins/{id}/start` | `Cabins.Start` |
| `post /api/cabins/{id}/stop` | `Cabins.Stop` |
| `post /api/cabins/{id}/reset` | `Cabins.Reset` |
| `get /api/cabins/{id}/agent-url` | `Cabins.AgentURL` |
| `post /api/cabins/{id}/terminal` | `Cabins.OpenTerminal` |
| `get /api/cabins/{id}/entry` | `Cabins.EntryFindings` |
| `get /api/cabins/{id}/changes` | `Cabins.ReviewItems` |
| `post /api/cabins/{id}/revert json {"path": "..."}` | `Cabins.Revert` |
| `post /api/cabins/{id}/apply json {"path": "..."}` | `Cabins.Apply` |
| `post /api/cabins/{id}/finish` | `Cabins.FinishReview` |
| `get /api/cabins/{id}/report` | `Cabins.Report` |
| `post /api/holds/{id} json {"allow": true}` | `Cabins.Decide` |
| `get /api/events` | `Cabins.Subscribe`，以 `text/event-stream` 输出 |

### 5.11 webterm

```go
package webterm

type Attacher interface {
	Attach(ctx context.Context, id, token string) (sandbox.Session, error)
}

// New 返回网页终端的 HTTP 处理器。origin 是网页终端自己的源，例如 http://127.0.0.1:<端口>。
func New(a Attacher, origin string) http.Handler
```

端口和令牌（[ADR-006](adr/006-embed-agent-ui.md) 要求本文档确定）：

1. `cmd/airlock` 启动时，在 `127.0.0.1` 上为网页终端分配一个空闲端口。这个端口与控制台不同，所以两者是不同的源。全部工作舱共用这个端口。
2. 每次运行开始时，`cabin` 生成一个随机的终端令牌。运行结束时，令牌失效。
3. `AgentURL` 返回 `<TermURL>/?cabin=<id>#token=<令牌>`。令牌在 URL 片段中，不发给服务器。
4. 网页终端页面连接 `/ws?cabin=<id>`，在 `Sec-WebSocket-Protocol` 请求头中提供两个子协议：`airlock` 和令牌。握手成功时，`webterm` 选择 `airlock`。
5. 握手时，`webterm` 检查 Origin 等于自己的源，再用令牌调用 `Attach`。`cabin` 用常量时间比较令牌。任一检查不通过时，`webterm` 拒绝握手。
6. 握手后，键盘输入和窗口大小以文本消息发送，终端输出以二进制消息返回。

- 网页终端不加载 `@xterm/xterm` 的链接插件和剪贴板插件。
- Go 标准库没有 WebSocket。`webterm` 使用 `github.com/coder/websocket`。这个库是纯 Go 实现，使用 ISC 许可证，握手时默认拒绝跨源请求。备选方案 `github.com/gorilla/websocket` 最近一次发布是 2024 年。
- 网页终端的前端放在 `web/` 中，单独构建到 `internal/webterm/dist`。

### 5.12 desktop

```go
package desktop

func OpenURL(url string) error
func OpenTerminal(argv []string) error
func PickFolder(ctx context.Context) (string, error) // 用户取消时返回空字符串
```

## 6. 关键流程

### 6.1 启动运行

1. `console` 调用 `cabin.Start`。
2. `cabin` 读取预设，调用 `Inspector.Entry` 做进舱检查。
3. `cabin` 调用 `staging.Create`。`Excluded` 的文件不复制。
4. `cabin` 打开本次运行的审计记录，生成终端令牌。
5. `cabin` 调用 `gate.Start`。`Host` 为 `airlock-gate:8080`，`Allow` 来自 `gate.TierHosts`，`Providers` 只含预设的供应商，`Secret` 为 `Options.Secret`。
6. 预设的界面为 web 时，`cabin` 在 `127.0.0.1` 上分配一个空闲端口作为 `HostPort`。
7. `cabin` 展开预设中的模板变量，调用 `Backend.Create`。
8. 预设的界面为 web 时，`cabin` 用 `Backend.Logs` 读取启动输出，用 `TokenPattern` 提取令牌，并校验令牌的格式。
9. `cabin` 把状态改为 `Running`，并发出 `status` 事件。
10. `console` 调用 `AgentURL`，把地址加载到运行页面左侧的 iframe 中。

### 6.2 暂扣

```
gate ──Ask(ctx, e)──► cabin ──hold 事件（模板解释）──► console ──► 拦截卡片
                        │
                        ├──Explain（最多 5 秒）──► advisor
                        └──explained 事件（AI 解释或 AIUnavailable）──► console

console ──Decide(id, allow)──► cabin ──Ask 返回──► gate
gate ──OnEvent(e)──► cabin ──► audit.Add，request 和 hold-closed 事件 ──► console
```

- `cabin` 先发出 `hold` 事件，再异步调用 `Explainer.Explain`。用户不需要等 AI 解释。
- `Explain` 返回 `ErrAdvisorOff` 时，`cabin` 不发出 `explained` 事件。
- `Explain` 返回其他错误时，`cabin` 发出 `explained` 事件，解释仍为模板解释，`AIUnavailable` 为 true。
- 用户在 AI 解释生成前做了决定时，`cabin` 丢弃之后的 AI 解释。
- `ctx` 取消时，`Ask` 返回 false，闸门记为 `timeout`。

### 6.3 网页终端

```
iframe ──WebSocket──► webterm ──Attach(id, token)──► cabin ──Exec──► sandbox ──► agent
```

- 网页终端断开时，`webterm` 关闭 `Session`。这只断开连接，agent 继续运行。用户刷新页面后重新连接，继续原来的对话。
- 用户点击"在终端窗口中打开"时，`cabin` 用 `Backend.AttachCommand` 和 `desktop.OpenTerminal` 打开独立终端窗口。终端窗口连接同一个 agent 会话。

### 6.4 运行中的改动

1. 运行页面每隔几秒调用一次 `cabin.ReviewItems`，在右侧列出改动，高风险改动标为红色。
2. 用户点击"撤销这个改动"时，`console` 调用 `cabin.Revert`，`cabin` 调用 `Area.Revert`。
3. 用户在左侧的 agent 界面中告诉 agent 换一种做法。Airlock 不替用户向 agent 发送指令。

### 6.5 出舱审查

1. `console` 调用 `cabin.Stop`。`cabin` 删除沙箱，关闭闸门，让终端令牌失效，把状态改为 `Review`。
2. `console` 调用 `cabin.ReviewItems`。`cabin` 调用 `Area.Changes` 和 `Inspector.Exit`，合并结果。
3. 默认全部应用。`console` 为用户没有丢弃的每个改动调用 `cabin.Apply`，`cabin` 调用 `Area.Apply`。用户丢弃了文件时，界面提示改动可能不完整。
4. 用户完成审查时，`console` 调用 `cabin.FinishReview`。`cabin` 为每个改动写入一条 `audit.Review`，关闭审计记录，调用 `Area.Discard`，把状态改为 `Idle`。
5. `console` 调用 `cabin.Report` 显示运行报告。

## 7. 数据存储

全部数据放在 `%LOCALAPPDATA%\Airlock`：

| 路径 | 内容 | 是否挂载进沙箱 |
|---|---|---|
| `cabins.json` | 工作舱列表 | 否 |
| `advisor.json` | 安全副驾的设置 | 否 |
| `cabins\<id>\files\` | 暂存区 | 是，挂载到 `/workspace` |
| `cabins\<id>\manifest.json` | 暂存区的基线清单 | 否 |
| `runs\<run ID>.jsonl` | 审计记录 | 否 |

真实密钥只存于 Windows 凭据管理器，不在这个目录中。

## 8. 分工

| 成员 | 负责的层 | 负责的目录 |
|---|---|---|
| `lijiahao818` | 核心层、入口层 | `internal/cabin`、`internal/gate`、`internal/sandbox`、`internal/staging`、`internal/vault`、`internal/audit`、`internal/desktop`、`presets/`、`cmd/airlock` |
| `pionxe` | 界面层、扩展层 | `internal/console`、`internal/webterm`、`web/`、`internal/advisor`、`internal/filecheck`、`evals/`、`Makefile` |
