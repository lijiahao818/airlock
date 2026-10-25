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
| 不会发生 | macOS 和 Linux 宿主机、MCP 管理、团队管理、云端沙箱、解密 HTTPS。 | 见 [product.md](product.md#6-范围)。不设计扩展点。 |
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

产品不管理 MCP（见 [product.md](product.md#6-范围)），所以不建立 `internal/mcphub`。

## 4. 模块

### 4.1 模块职责

每个模块写明它做什么，也写明它不做什么。

| 模块 | 目录 | 做什么 | 不做什么 |
|---|---|---|---|
| cabin | `internal/cabin` | 管理工作舱和运行的生命周期。把暂扣请求转给用户。用规则模板生成暂扣解释。 | 不做出网判定。不读写项目文件的内容。不调用模型。 |
| gate | `internal/gate` | 监听出网请求。按档位和用户决定放行或拒绝。在模型路由上把占位符换成真实密钥。 | 不知道工作舱、界面和安全副驾。不保存密钥。不记录请求体、请求头和查询参数。 |
| sandbox | `internal/sandbox` | 创建和删除沙箱、中继容器和 internal 网络。构建镜像。在沙箱中打开终端会话。管理工作舱的依赖缓存卷。 | 不知道 agent 的种类。不处理密钥。 |
| staging | `internal/staging` | 把项目复制到暂存区。列出改动。撤销单个改动。把单个改动安全地写回项目。 | 不判断改动的风险。不决定哪些文件不复制。 |
| vault | `internal/vault` | 在系统钥匙串中保存、读取、删除密钥。 | 不验证密钥是否可用。不把密钥写入文件。 |
| audit | `internal/audit` | 记录一次运行的出网判定和出舱审查决定。生成运行报告。 | 不做判定。不记录请求内容。 |
| presets | `presets/` | 用声明文件描述每个 agent：镜像、命令、环境变量、配置文件、模型供应商、界面类型。 | 不包含真实密钥。 |
| desktop | `internal/desktop` | 打开浏览器、打开终端窗口、弹出文件夹选择框。 | 不包含业务逻辑。 |
| filecheck | `internal/filecheck` | 进舱检查：找出不能复制的文件、依赖目录和需要注意的文件。出舱审查：标出高风险改动。 | 不复制文件。不写回项目。 |
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
// ResetDecision 撤销本次运行中对 hostPort（例如 example.com:443）的决定，并断开这个主机这个端口已建立的连接。
func (g *Gate) ResetDecision(hostPort string)

func Providers() []string                       // anthropic、openai、deepseek
func Tiers() []string                           // strict、standard、permissive
func TierHosts(tier string) ([]string, error)   // 档位自动放行的主机
func TestKey(ctx context.Context, provider, secret string) error // 用密钥调用供应商的接口
```

模型路由：

| 供应商 | 闸门路由 | 上游地址 | 允许的路径 |
|---|---|---|---|
| anthropic | `/anthropic` | `https://api.anthropic.com` | `/v1/messages`、`/v1/messages/count_tokens` |
| openai | `/openai` | `https://api.openai.com` | `/v1/responses` |
| deepseek | `/deepseek` | `https://api.deepseek.com/anthropic` | `/v1/messages` |

- 模型路由只转发表中的路径，只接受 POST。路径不含查询参数。
- 密钥还能访问同一账号下的文件和批处理等接口。预设的 agent 和安全副驾只需要推理接口，所以其他路径一律拒绝。

判定规则（`Event.Rule` 的取值）：

| Rule | 条件 | 结果 |
|---|---|---|
| `route` | POST 请求发往开放的模型路由的允许路径，并携带本实例签发的占位符。 | 放行，换上真实密钥。 |
| `bad-placeholder` | 请求发往模型路由，但占位符不对。 | 拒绝。 |
| `route-path` | 占位符正确，但路径不在允许的路径中，或者方法不是 POST。 | 拒绝，不连接上游。 |
| `no-key` | 请求发往开放的模型路由，占位符和路径正确，但闸门读取不到真实密钥。 | 拒绝。 |
| `bad-host` | 非 CONNECT 请求的 Host 头不等于 `Config.Host`。 | 拒绝。 |
| `unsupported` | 既不是 CONNECT，也不是模型路由。 | 拒绝。 |
| `busy` | 同时进行的 CONNECT 已经达到 400 个，包括等待用户决定的暂扣和已建立的隧道。 | 立即拒绝，返回 503。不询问用户。 |
| `tier` | CONNECT 的目标主机在 `Config.Allow` 中，端口为 443。 | 放行。 |
| `run-allow` | 用户在本次运行中允许过这个主机的这个端口。 | 放行。 |
| `run-deny` | 用户在本次运行中拒绝过这个主机的这个端口。 | 拒绝，不再询问。 |
| `user-allow` | 暂扣后用户允许。 | 放行。以后记为 `run-allow`。 |
| `user-deny` | 暂扣后用户拒绝。 | 拒绝。以后记为 `run-deny`。 |
| `timeout` | 暂扣后 `HoldTimeout` 内没有决定。 | 拒绝。下次请求再次询问。 |
| `private-address` | CONNECT 的目标是非公网的 IP 地址或 `localhost`。或者 CONNECT 已被放行，但目标主机解析出的地址不是公网地址。 | 拒绝。不改变用户的决定。 |

- 暂扣时，传给 `Ask` 的事件中 `Rule` 为 `hold`，`Allowed` 为 false。
- 每个 CONNECT 占用出网中继的一个子进程。CONNECT 上限 400 低于中继的子进程上限 480，所以暂扣和下载再多，中继也留有余量转发模型请求。
- 用户的决定按主机和端口记录。允许或拒绝 `example.com:443` 后，`example.com:22` 的请求仍然暂扣。同一主机的不同端口风险不同，例如 22 是 SSH，25 是 SMTP。
- 同一个主机同一个端口的并发暂扣只调用一次 `Ask`。
- 一个闸门实例对应一次运行。实例关闭后，`run-allow` 和 `run-deny` 一起失效。
- `ResetDecision` 删除这个主机这个端口的 `run-allow` 或 `run-deny`，下一个请求再次暂扣。撤销 `run-allow` 时，闸门关闭这个主机这个端口正在使用的 CONNECT 隧道，否则撤销不会立即生效。同一主机其他端口的决定和隧道不受影响。参数不是“主机:端口”的格式时，什么也不撤销。`ResetDecision` 只由用户点击触发，它只会让请求再次暂扣，不会放行请求。
- 闸门只在 CONNECT 被放行后解析目标主机。被拒绝的主机名不会触发 DNS 查询，所以 agent 不能借闸门的 DNS 查询外带数据。
- 解析出的地址中有一个不是公网地址时，闸门拒绝请求，规则记为 `private-address`。目标是 IP 地址、`localhost` 或以 `.localhost` 结尾的主机名时，闸门在暂扣前检查。不是公网地址就直接拒绝，不调用 `Ask`，也不查询 DNS。
- IANA 特殊用途地址表把一部分地址标为不能全局访问。公网地址是全局单播地址，并且不在这些标出的范围中。例如本机、局域网、链路本地地址和 `100.64.0.0/10` 都不是公网地址。
- 暂扣后用户允许，但地址检查拒绝时，闸门先发出 `user-allow` 事件记录用户的决定，再发出 `private-address` 事件。
- 闸门只连接检查过的地址，不再次解析主机名，所以 DNS 重绑定不能绕过检查。
- 上游返回非 2xx 响应时，闸门把响应体中长度不少于 4、又是真实密钥子串的文字换成 `****`。响应体超过 1 MiB 或带压缩编码时，闸门整个换成固定文字。闸门转发请求时去掉 `Accept-Encoding`，所以上游的压缩响应由闸门解压后再检查。
- 路由表的上游地址不导出。单元测试在包内把上游替换为本地 mock。

### 5.2 sandbox

```go
package sandbox

type Spec struct {
	Name       string            // 由工作舱 ID 生成，每次运行相同。只含小写字母、数字和连字符
	Image      string
	Dockerfile []byte            // 镜像不存在时用它构建
	StagingDir string            // 宿主机上的暂存区目录，挂载到 /workspace
	Env        map[string]string // 只含占位符和地址，不含真实密钥
	Files      map[string]string // 沙箱内的路径 → 文件内容，agent 启动前写入
	Cmd        []string          // 容器的主进程。为空时运行空闲进程
	GatePort   int               // 闸门在宿主机上的端口，出网中继转发到这里
	WebPort    int               // 沙箱内网页服务的端口。0 表示没有网页界面
	HostPort   int               // 入站中继在宿主机 127.0.0.1 上的端口
	Volumes    []string          // 暂存区内的相对目录，每个挂载为这个工作舱专用的缓存卷
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
	Prune(ctx context.Context) error                                   // 删除 Airlock 创建的全部残留资源，不删除缓存卷
	RemoveCache(ctx context.Context, name string) error                // 删除 Spec.Name 为 name 的工作舱的全部缓存卷
	Health(ctx context.Context, id string) error                       // 都在运行时返回 nil。确认停止时返回 *StoppedError，检查失败时返回其他错误
}

func Docker() Backend // 调用 docker 命令行实现，不引入 Docker SDK
```

- `Create` 按 [ADR-001](adr/001-isolation-backend.md) 的参数启动沙箱。
- 沙箱只接入 internal 网络。出网中继在 internal 网络上的别名为 `airlock-gate`，监听 8080 端口。
- `WebPort` 不为 0 时，`Create` 启动入站中继，把 `127.0.0.1:<HostPort>` 映射到沙箱的 `WebPort`。
- `Create` 给全部资源加上标签 `airlock=1`。`Prune` 按这个标签删除资源。
- terminal 型的 agent 运行在沙箱内的 tmux 会话中。`Exec` 和 `AttachCommand` 连接同一个会话，所以网页终端和独立终端窗口看到的是同一个 agent。断开连接不结束 agent。删除沙箱时 agent 结束。terminal 型预设的镜像必须安装 tmux。
- `Volumes` 中的每个目录挂载为一个 Docker 卷（[ADR-008](adr/008-dependency-cache-volumes.md)）。卷名由 `Spec.Name` 和目录路径的哈希组成，所以同一个工作舱的下一次运行使用同一组卷。卷带有标签 `airlock-cache=1`。
- `Exec` 的宿主机一侧没有控制台，`docker exec -t` 在这种情况下会失败。`Exec` 用 `docker exec -i` 运行沙箱内的 `script`，由 `script` 分配终端。终端的第一行输出是它的设备名，`Exec` 读出后去掉这一行。`Resize` 在沙箱内对这个设备运行 `stty`。
- `Session.Close` 先用 `tmux detach-client` 断开这个终端，再关闭输入。`script` 在输入结束时会向终端写入结束符。`Exec` 在连接前用 `stty eof undef` 去掉这个字符。所以宿主机进程异常退出时，agent 不会收到 `^D`。
- 每个工作舱有两个网络：internal 网络 `airlock-<Name>-in` 和普通网络 `airlock-<Name>-out`。沙箱只接入前者。中继接入两者，所以中继不和其他容器共用网络。
- 中继镜像是 `alpine:3.20` 加上 `socat`，基础镜像按 digest 锁定，以 `nobody` 用户运行。Dockerfile 内嵌在 `sandbox` 包中，首次使用时在本地构建。出网中继把 8080 端口转发到 `host.docker.internal:<GatePort>`。
- socat 为每个连接启动一个子进程。暂扣中的连接最长占用子进程 60 秒，依赖下载和模型的流式响应也同时占用子进程。中继的 `--pids-limit` 为 512，`--memory` 为 256m，socat 的 `max-children` 为 480。子进程数达到上限时，socat 暂停接受新连接，主进程不退出。连接数降下来后，排队的连接继续建立。实测 socat 1.8.0.0 在达到上限时还会多接受一个连接。
- 沙箱以 `1000:1000` 用户运行，`HOME` 为 `/home/agent`。`/home/agent` 和 `/tmp` 是 tmpfs，`/tmp` 允许执行文件。
- `Spec.Env` 通过临时的 env 文件传给 docker，创建容器后删除这个文件。`Spec.Files` 打包为 tar，通过 `docker exec` 的标准输入写入。它们都不出现在 docker 的进程参数中。`Files` 的路径可以用 `~/` 表示 `/home/agent/`。
- 沙箱的主进程先等待 `Files` 写入完成，再运行 `Spec.Cmd`。所以 agent 启动时配置文件已经存在。
- 沙箱镜像必须包含 `sh`、`tar`、`sleep`。terminal 型预设的镜像还必须包含 `tmux` 和 `script`。

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
	Special  bool   // 符号链接、特殊文件，或 Windows 不能正确读写的文件名，不能写回项目
	Applied  bool   // 改动已经写回项目
}

type Op string // 差异中一行的类型：add、del、same、skip

type Line struct {
	Op   Op
	Text string // 不含换行符
	Skip int    // Op 为 skip 时省略的不变行数
}

type Diff struct {
	Lines  []Line
	Reason string // 不显示内容的原因。不为空时 Lines 为空
}

type Area struct{ /* 未导出 */ }

// Create 在 dir 下建立暂存区，把 project 复制进去。skip 返回 true 的文件不复制。
// rewrite(rel) 返回的函数改写这个文件复制进沙箱的内容。返回 nil 表示原样复制。rewrite 可以为 nil。
func Create(project, dir string, skip func(rel string) bool, rewrite func(rel string) func(content []byte) []byte, progress func(done, total int)) (*Area, error)
func Open(project, dir string, rewrite func(rel string) func(content []byte) []byte) (*Area, error) // 打开已有的暂存区
func (a *Area) Files() string                 // 挂载进沙箱的目录
func (a *Area) Changes() ([]Change, error)
func (a *Area) Diff(rel string) (Diff, error) // 一个改动的内容差异
func (a *Area) Revert(rel string) error       // 撤销一个改动，恢复到基线
func (a *Area) Apply(rel string) error        // 把一个改动写回项目
func (a *Area) Discard() error                // 删除整个暂存区
```

复制和比较的方法（[ADR-003](adr/003-exit-review-staging.md) 要求本文档确定）：

1. `Create` 把项目文件复制到 `dir/files`，包括 `.git` 目录。`skip` 返回 true 的文件和目录不复制。项目中的符号链接和特殊文件不复制，也不跟随，因为它们可能指向项目以外。每复制一批文件，`Create` 调用一次 `progress`。
2. `Create` 把每个复制的文件的 SHA-256 写入 `dir/manifest.json`。这个文件是基线清单，不挂载进沙箱。改写过的文件，基线存改写后内容的哈希，另存改写前内容的哈希。
3. `Changes` 遍历 `dir/files`，与基线清单比较，得出新增、修改和删除的文件。
   - Linux 允许路径段以点或空格结尾，但 Windows 打开这类路径时去掉末尾的字符，读写的是另一个文件。`Changes` 不读取这类文件，把它标为 `Added` 和 `Special`。这类目录不进入，目录本身作为一个改动列出。`Apply` 和 `Revert` 拒绝这类路径。
   - 不在基线清单中的文件一定是新增，`Changes` 不计算它的哈希。
   - `Changes` 在内存中记下暂存区文件的哈希和身份信息。身份信息是大小、修改时间、ctime（状态改变时间）和 inode（Windows 上是创建时间）。下次调用时，身份信息没有变化的文件沿用这个哈希。
   - agent 可以把大小和修改时间改回原样，但改不了 ctime：写入文件时，内核或 NTFS 一定更新它。所以这个缓存不会漏掉内容的改动。Windows 上，`Changes` 用 `GetFileInformationByHandleEx` 一次读取一个目录中全部条目的身份信息，不逐个打开文件。
   - 2 秒内改过的文件不缓存哈希，因为同一个时钟刻度内的两次写入，ctime 可能相同。Linux 和 Windows 以外的系统不缓存哈希。
4. `Changes` 计算项目文件夹中对应文件的 SHA-256。结果与基线不同时，标记 `Conflict`。暂存区新增的文件在项目中已经存在时，也标记 `Conflict`。改写过的文件与改写前内容的哈希比较。
5. `Apply` 拒绝 `Conflict` 和 `Special` 的改动。`Apply` 拒绝越出项目文件夹的路径。`Apply` 不跟随符号链接。
   - 写回成功后，`Apply` 把路径记入 `dir/applied.json`。这个文件不挂载进沙箱。`Changes` 把记录中的改动标为 `Applied`，不再判断冲突。
   - 对 `Applied` 的改动再次调用 `Apply` 时，`Apply` 不修改项目，返回成功。所以部分改动写回失败后，用户可以重试整个审查。
6. `Revert`（[ADR-007](adr/007-review-during-run.md)）只从基线清单中的文件恢复：把项目中的同名文件复制回暂存区。改写过的文件，`Revert` 把项目中的同名文件改写后复制回暂存区，不在磁盘保存改写后的内容。不在基线清单中的文件，`Revert` 从暂存区删除。所以进舱检查排除的文件不会被复制进暂存区。
7. `Revert` 拒绝 `Conflict` 的改动，因为项目中的文件已经不是基线。`Revert` 拒绝越出暂存区的路径，不跟随符号链接。agent 新建的 `.env` 与项目中被排除的 `.env` 冲突，所以运行中不能撤销。出舱审查时 `Apply` 也拒绝它，它不会写入项目。
8. 全部文件操作都经过 `os.Root`，路径不能经过 `..` 或符号链接越出项目文件夹和暂存区。
9. `Apply` 和 `Revert` 拒绝含 `:` 或 `\` 的路径。这两个字符在 Linux 文件名中合法，但在 Windows 上分别表示数据流和路径分隔符。agent 在沙箱中创建这类文件时，`Changes` 会列出它，但它不能写回项目，也不能撤销。
10. `Diff` 逐行比较项目中现在的文件和暂存区中 agent 的版本。写回后，项目中的文件就是 agent 的版本。没有冲突时，项目中现在的文件就是基线。
    - 新增的文件全部列为新增的行。删除的文件全部列为删除的行。每处改动前后保留 3 行不变的内容，其余不变的行合并为一个 `skip` 行。
    - `Diff` 和 `Apply` 一样校验路径，并只读取 `Changes` 列出的改动。符号链接、特殊文件、越出暂存区的路径、二进制文件、不是 UTF-8 的文件和超过 1 MB 的文件不读取内容，`Reason` 用中文说明原因。
    - 内容来自沙箱，属于不可信数据。前端只把它作为文本渲染，不渲染 HTML，也不把网址变成链接。

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
	Hosts   []HostSummary  // agent 访问过的主机和端口
	Holds   []HoldSummary  // 暂扣过的主机、端口和用户的决定
	KeyUses map[string]int // 供应商 → 换上真实密钥的次数
	Files   []Review       // 改动的文件和用户的决定
}

// HostSummary 是一个主机的一个端口的统计。同一主机的不同端口分开统计。
type HostSummary struct {
	Host            string
	Port            int
	Allowed, Denied int
}

type HoldSummary struct {
	Host string
	Port int
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
	Languages  []string          // 沙箱镜像中的语言环境
	UI         string            // terminal 或 web
	Cmd        []string          // terminal：在终端中运行的命令。web：容器的主进程
	Env        map[string]string // 值中可以使用模板变量
	Files      map[string]string // 沙箱内的路径 → 内容，可以使用模板变量
	Web        *Web              // UI 为 web 时必填
	Dockerfile []byte
}

type Web struct {
	Port         int    // 沙箱内网页服务的端口
	TokenPattern string // 从启动输出的每一行中提取令牌的正则表达式，必须有一个分组
	OpenPath     string // 网页界面的路径，以 / 开头并含 {token}，例如 /?token={token}
}

func All() ([]Preset, error)
func Get(id string) (Preset, error)
```

模板变量：

| 变量 | 值 |
|---|---|
| `{gate}` | `http://airlock-gate:8080` |
| `{placeholder}` | 闸门为这个供应商签发的占位符 |
| `{placeholder-tail}` | 占位符的最后 20 个字符。Claude Code 用这个长度识别用户批准过的 API key |
| `{webhost}` | `127.0.0.1:<HostPort>` |
| `{token}` | 从启动输出中提取的令牌，只用于 `OpenPath` |

`Web` 的规则：

- 只有 web 型预设有 `Web`。terminal 型预设不能有 `Web`。
- `cabin` 把启动输出按行拆开，去掉行尾的 `\r\n` 或 `\n`，再对每一行应用 `TokenPattern`。所以 `^` 和 `$` 表示一行的开头和结尾。`cabin` 使用第一个匹配的行。
- `OpenPath` 必须以 `/` 开头，并且含 `{token}`。`cabin` 把它拼在 `http://127.0.0.1:<HostPort>` 后面，所以 `OpenPath` 不能改变主机。

`Languages` 的规则：

- `Languages` 声明镜像中装有的语言环境，不能为空。取值只能是：`node`、`python`、`go`、`rust`、`java`、`c`、`cpp`。
- 三个预设的镜像都基于 `node:24-bookworm-slim`，并安装 `python3`，所以声明为 `["node", "python"]`。agent 以普通用户运行，不能自己安装其他语言。
- `console` 用相同的取值检测项目使用的语言（第 5.10 节）。项目用了声明之外的语言时，新建页面提示用户。

Claude Code 预设示例：

```json
{
  "id": "claude-code",
  "name": "Claude Code",
  "provider": "anthropic",
  "languages": ["node", "python"],
  "ui": "terminal",
  "cmd": ["claude"],
  "env": {
    "ANTHROPIC_BASE_URL": "{gate}/anthropic",
    "ANTHROPIC_API_KEY": "{placeholder}",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
    "CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL": "1",
    "HTTPS_PROXY": "{gate}"
  },
  "files": {
    "~/.claude.json": "{\"hasCompletedOnboarding\": true, \"customApiKeyResponses\": {\"approved\": [\"{placeholder-tail}\"], \"rejected\": []}}\n"
  }
}
```

- Claude Code 首次启动时运行引导。引导检查到 `platform.claude.com` 的连接，失败时退出。安全档位不放行这个主机，所以预设写入 `~/.claude.json`，标记引导已完成。
- Claude Code 只使用 `~/.claude.json` 中批准过的 `ANTHROPIC_API_KEY`，否则询问用户，默认答案为不使用。所以预设预先批准占位符。占位符不是密钥，离开闸门后没有用。
- Codex 默认把后台程序解压到 `~/.codex/packages`，再运行它。`/home/agent` 不允许执行文件，所以预设用 `--no-daemon` 启动 Codex，不使用后台程序。
- Claude Code 启动时自动安装官方插件市场，连接 `downloads.claude.ai` 和 `github.com`。`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` 不关闭这些请求，所以预设设置 `CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL=1`。
- Codex 启动时同步插件，连接 `chatgpt.com`、`github.com` 和 `api.github.com`。预设在 `config.toml` 中设置 `[features] plugins = false`，关闭插件功能。
- Codex 启动时还从 `raw.githubusercontent.com` 读取公告。Codex 没有关闭这个请求的设置，所以预设把这个主机写入 `NO_PROXY`。请求不经过闸门，沙箱在 internal 网络中，所以连接在沙箱内失败。
- Codex 默认在 bubblewrap 沙箱中执行命令。bubblewrap 要创建命名空间，但 Airlock 的沙箱删除了全部 Linux 能力，所以命令全部失败。预设设置 `sandbox_mode = "danger-full-access"`，由 Airlock 的容器做隔离边界。容器的加固参数不变。
- 预设设置 `approval_policy = "on-request"`。Codex 直接执行大多数命令，认为有风险时先问用户。文件改动仍然经过出舱审查，出网仍然经过闸门。
- DeepSeek Harness 首次启动时，在系统的"文档"目录中创建默认工作区。沙箱中没有"文档"目录，所以创建失败，用户要手动选择文件夹。预设写入 `~/.dsh/storages/workspace.json`，把 `/workspace` 注册为默认工作区。家目录中的改动不在暂存区中，停止后丢失。

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
	Cached   FindingKind = "cached"    // 依赖目录：不复制到暂存区，改用缓存卷
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
	Kind    string // request、hold、explained、hold-closed、status 或 progress
	Cabin   string
	Request *gate.Event // Kind 为 request
	Hold    *Hold       // Kind 为 hold、explained 或 hold-closed
	Status  Status      // Kind 为 status
	Progress *Progress  // Kind 为 progress
}

// Progress 是启动时复制项目文件的进度。
type Progress struct {
	Done  int
	Total int
}

// Service 是界面层使用的全部功能。
type Service interface {
	CheckDocker(ctx context.Context) error

	List() ([]Cabin, error)
	Create(project, preset, tier string) (Cabin, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Reset(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error // 只在 Idle 状态可用

	AgentURL(id string) (string, error)  // 嵌入运行页面的 agent 界面地址
	OpenTerminal(id string) error        // 保底方式：在独立终端窗口中打开 agent
	Attach(ctx context.Context, id, token string) (sandbox.Session, error) // 网页终端使用

	EntryFindings(id string) ([]Finding, error)
	ReviewItems(id string) ([]ReviewItem, error) // Running 和 Review 状态都可以调用
	Diff(id, path string) (staging.Diff, error)  // Running 和 Review 状态都可以调用
	Revert(id, path string) error               // 只在 Running 状态可用
	Apply(id, path string) error                // 只在 Review 状态可用
	FinishReview(id string) error
	Report(id string) (audit.Report, error) // 最近一次运行的报告

	// Holds 返回工作舱正在等待用户决定的暂扣。运行页打开或刷新时，用它补上错过的拦截卡片。
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
- `Reset` 丢弃暂存区，调用 `Backend.RemoveCache` 删除缓存卷，不生成运行报告。
- `Delete` 只在 `Idle` 状态可用。它先删除缓存卷和暂存区目录，再从工作舱列表中删除这个工作舱。项目文件夹和运行记录不受影响。删除缓存卷、暂存区或保存列表失败时，工作舱保留，用户可以再次删除。
- 三种状态都可以 `Reset`。`Idle` 状态的工作舱仍然有上次运行的缓存卷，用户用 `Reset` 删除沙箱中安装的依赖（F-3）。
- `Reset` 后工作舱没有运行报告，`RunID` 为空，`Report` 返回错误。
- 一个工作舱的 `Start`、`Stop`、`Reset`、`FinishReview`、`Revert`、`Apply` 不能同时进行。前一个操作没有结束时，后一个返回错误。所以 `console` 要逐个调用 `Apply`，不能并发调用。
- 删除沙箱或缓存卷失败时，`Stop` 和 `Reset` 返回错误，用户可以再试一次。删除沙箱失败时状态不变。`Running` 时 `Reset` 先完成 `Stop`，之后删除缓存卷失败时，工作舱停在 `Review`，因为运行已经结束。
- `FinishReview` 写入审查记录后回到 `Idle`。删除沙箱后的几秒内，Docker Desktop 仍占用暂存区目录。所以删除暂存区失败时，`FinishReview` 不返回错误，`cabin` 在后台重试删除。
- 后台重试删除前把工作舱标记为忙，删除时不持有 `Manager` 的锁。工作舱不再是空闲状态时，重试停止，所以不会删除新一次运行的暂存区。
- `New` 删除不属于 `Review` 状态工作舱的暂存区目录。下次 `Start` 也会先删除残留的暂存区。
- `New` 先调用 `Backend.Prune`，再把 `Running` 状态的工作舱改为 `Review`。
- `StopAll` 并发停止全部运行中的工作舱，由 `cmd/airlock` 在退出时调用。ctx 到期时，没有停完的工作舱状态不变。
- `StopAll` 开始后，`Start` 返回错误。退出期间控制台仍在监听，这条规则防止新的沙箱在退出后继续运行。
- 启动失败时，`cabin` 删除这次运行的审计记录文件，`runs` 目录中不留下空记录。
- `Start` 在构建镜像前检查 `Options.Secret` 能否取到预设供应商的密钥。取不到时返回中文错误，说明缺少哪家的密钥和去哪里添加，不创建沙箱。
- 运行期间，`cabin` 每 3 秒调用一次 `Backend.Health`，超时 3 秒。docker 确认出网中继或沙箱停止时，`cabin` 发出 `problem` 事件，`List` 的结果中这个工作舱的 `Problem` 为中文说明，运行页显示它。这个字段不保存。检查本身失败时（例如 docker 暂时没有响应），下一次再查，不报告问题。运行结束后，检查在下一次时退出。
- `List` 的结果中，正在启动的工作舱 `Starting` 为 true。刷新页面后，运行页靠它继续显示启动进度。这个字段不保存到 `cabins.json`。
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
func (a *Advisor) ModelConfig(provider string) (model, effort string, ok bool) // 只读，供报告使用
```

- 模型调用经过一个专用的闸门实例。这个实例只开放 anthropic 和 deepseek 路由，拒绝全部 CONNECT。
- 供应商按 F-11 选择：只有一家的密钥时用这一家。两家都有时用 `Settings.Provider`，`Provider` 为空时用 `anthropic`。两家都没有时返回 `cabin.ErrAdvisorOff`。
- 提示词放在 `internal/advisor/prompt.md`，模型 ID 放在 `internal/advisor/config.json`，都用 `embed` 打包。提示词版本是提示词的 SHA-256 的前 12 位。
- 同一次运行中，同一主机、端口和规则的解释只生成一次。缓存键含端口和规则，因为同一主机的不同端口风险不同。每次运行最多调用模型 20 次。
- 输出不符合 schema、超过长度上限、超时或调用失败时，返回错误。
- 产品评估集见 5.13 节，用 `make eval` 运行。

### 5.9 filecheck

```go
package filecheck

type Checker struct{}

func (Checker) Entry(project string) ([]cabin.Finding, error)
func (Checker) Rewrite(rel string) func(content []byte) []byte
func (Checker) Exit(project, files string, changes []staging.Change) []cabin.Finding
```

- `Entry` 把 `.env` 文件和私钥文件标为 `Excluded`。
- `Entry` 把常见凭据文件标为 `Excluded`：`.npmrc`、`.yarnrc.yml`、`.pypirc`、`.netrc`、`.git-credentials`、`.aws/credentials`、`.docker/config.json`，以及 `.p12`、`.pfx`、`.jks`、`.keystore`、`.tfvars` 后缀的文件。
- `Rewrite` 改写 Git 远程配置：`.git/config` 和子模块的 `.git/modules/*/config`，去除远程地址中的用户信息。`Entry` 读到带凭据的远程配置时标为 `Notice`。
- `Entry` 把符号链接和特殊文件标为 `Excluded`，不跟随链接，也不读取目标。
- `Entry` 把 [ADR-008](adr/008-dependency-cache-volumes.md) 列出的依赖目录标为 `Cached`。目录的 `Path` 以 `/` 结尾，目录下的文件都不复制。
- `Exit` 把依赖目录下的改动标为 `Cached`。`cabin` 不在 `ReviewItems` 中列出这些改动，也不把它们写回项目。
- `Entry` 把 agent 会当作指令读取的文件标为 `Notice`，例如 `AGENTS.md`、`CLAUDE.md`、skill 文件。
- `Exit` 把 `.git` 目录、Git 钩子目录、CI 配置、依赖安装脚本、可执行文件的改动标为 `HighRisk`。
- `Exit` 把能自动执行命令的配置的改动标为 `HighRisk`：`.envrc`、`.vscode/tasks.json`、`.devcontainer/` 下的文件。agent 会当作指令读取的文件在 `Exit` 中也标为 `HighRisk`。
- `package.json` 只在安装生命周期脚本有新增或变化时标为 `HighRisk`。安装生命周期脚本是 `preinstall`、`install`、`postinstall`、`preprepare`、`prepare`、`postprepare`、`prepublish`。判定时读取项目文件夹中的同名文件作为基线。暂存区中的文件读不到、超过上限或解析失败时直接标为 `HighRisk`。基线读不到时按没有脚本处理。
- `Exit` 按文件头判断新增和修改过的可执行文件。`Exit` 只读取文件开头的几个字节，不跟随符号链接，读取范围不超出暂存目录。
- `Exit` 不读取含 `..` 或绝对路径的改动，直接标为 `HighRisk`。
- 名单匹配不区分大小写。宿主机的文件系统不区分大小写，区分大小写的匹配会被大小写变体绕过。

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
	DataDir    string // 向导是否完成记录在这里
}

// New 返回控制台的 HTTP 处理器。token 是启动时生成的随机令牌。
func New(d Deps, token string) http.Handler
```

- 每个 `/api` 请求都必须带请求头 `X-Airlock-Token`。
- `console` 调用 `Start`、`Stop`、`Reset` 时，使用不随请求取消的 ctx，超时为 20 分钟。浏览器刷新或关闭页面不会中断这些操作。第一次启动要构建镜像，可能要几分钟。
- Host 头只接受 `127.0.0.1:<端口>` 和 `localhost:<端口>`。
- `cmd/airlock` 用 `http://127.0.0.1:<端口>/#token=<令牌>` 打开浏览器。令牌在 URL 片段中，不发给服务器，也不进入服务器日志。
- 前端用 `fetch` 读取事件流，这样事件流也能带令牌请求头。
- 响应中的列表字段没有元素时输出 `[]`，不输出 `null`。
- 返回给用户的错误信息去掉 `cabin: `、`staging: ` 等包名前缀。日志和测试中的错误信息不变。
- 前端的数据类型由 `go run ./tools/tsgen` 从契约代码生成，写入 `web/src/generated/`。不要手动修改这些文件。修改契约代码后，重新生成并一起提交。
- 运行页面的左侧用 iframe 加载 `AgentURL` 的地址。右侧显示出网动态和拦截卡片。
- 收到 `hold` 事件时立即显示卡片。收到同一 ID 的 `explained` 事件时替换解释。
- 事件流不回放已经发出的事件。运行页打开或刷新时，用 `Cabins.Holds` 补上拦截卡片，用 `Cabins.Report` 补上出网动态。
- 导航栏轮询运行中工作舱的待处理暂扣。用户在其他页面时，导航栏显示提示，点击后进入对应的运行页。
- 前端构建结果输出到 `internal/console/dist`，用 `//go:embed all:dist` 打包。仓库在这个目录中只放占位文件 `.gitkeep`，构建结果不进仓库。`npm run build` 清空目录后补回 `.gitkeep`，工作区保持干净。没有构建前端时，静态页面返回代码中的中文提示页。
- 新建页面选好项目文件夹和预设后，前端调用 `post /api/project-languages` 检测项目使用的语言。检测只看文件名，不读文件内容；跳过依赖目录和 `.git`，最多查看 4096 个条目（文件和目录都算），请求取消时停止。项目用了预设没有声明的语言时，页面提示用户，但不阻止创建。
- 向导是否完成记录在数据目录的 `wizard-done` 文件中。控制台每次启动的端口不同，浏览器的 `localStorage` 不能跨次保存。数据目录中已经有工作舱时，也算完成。

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
| `post /api/project-languages json {"path": "..."}` | 检测项目文件夹使用的语言 |
| `get /api/cabins` | `Cabins.List` |
| `post /api/cabins json {"project": "...", "preset": "...", "tier": "..."}` | `Cabins.Create` |
| `post /api/cabins/{id}/start` | `Cabins.Start` |
| `post /api/cabins/{id}/stop` | `Cabins.Stop` |
| `post /api/cabins/{id}/reset` | `Cabins.Reset` |
| `post /api/cabins/{id}/delete` | `Cabins.Delete` |
| `get /api/cabins/{id}/agent-url` | `Cabins.AgentURL` |
| `post /api/cabins/{id}/terminal` | `Cabins.OpenTerminal` |
| `get /api/cabins/{id}/entry` | `Cabins.EntryFindings` |
| `get /api/cabins/{id}/changes` | `Cabins.ReviewItems` |
| `get /api/cabins/{id}/diff?path=...` | `Cabins.Diff` |
| `post /api/cabins/{id}/revert json {"path": "..."}` | `Cabins.Revert` |
| `post /api/cabins/{id}/apply json {"path": "..."}` | `Cabins.Apply` |
| `post /api/cabins/{id}/finish` | `Cabins.FinishReview` |
| `get /api/cabins/{id}/report` | `Cabins.Report` |
| `get /api/cabins/{id}/holds` | `Cabins.Holds` |
| `post /api/holds/{id} json {"allow": true}` | `Cabins.Decide` |
| `post /api/cabins/{id}/reset-decision json {"host": "example.com:443"}` | `Cabins.ResetDecision` |
| `get /api/events` | `Cabins.Subscribe`，以 `text/event-stream` 输出 |
| `get /api/wizard` | 返回向导是否已经完成 |
| `post /api/wizard` | 记录向导已经完成 |

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
7. 文本消息是一个 JSON 对象。键盘输入为 `{"input": "<文字>"}`，窗口大小为 `{"cols": <列数>, "rows": <行数>}`。行数和列数在 1 到 1000 之间。
8. 一条消息最大 64 KiB。消息超过上限、不是文本、不是上面两种格式时，`webterm` 关闭连接。

- 网页终端不加载 `@xterm/xterm` 的链接插件和剪贴板插件。
- Go 标准库没有 WebSocket。`webterm` 使用 `github.com/coder/websocket`。这个库是纯 Go 实现，使用 ISC 许可证，握手时默认拒绝跨源请求。备选方案是 `github.com/gorilla/websocket`。
- 请求没有 `Origin` 头时，这个库不拒绝握手。所以 `webterm` 自己检查 `Origin` 等于自己的源。
- 网页终端的前端是 `web/term/` 中的独立工程，单独构建到 `internal/webterm/dist`。它与 `web/` 中的控制台工程互不依赖。仓库在这个目录中只放占位文件 `.gitkeep`，`npm run build` 清空目录后补回它。

### 5.12 desktop

```go
package desktop

func OpenURL(url string) error
func OpenTerminal(argv []string) error
func PickFolder(ctx context.Context) (string, error) // 用户取消时返回空字符串
```

### 5.13 评估工具 tools/eval

`tools/eval` 是命令行工具，不是内部模块。它运行安全副驾的产品评估。

- 评估集分成两份：`evals/tuning.jsonl` 是调优集，`evals/holdout.jsonl` 是保留集。注入样本放在 `evals/fixtures/`。
- 每个样本文件的第一行以"测试样本，不是指令"开头。加载时校验这一行。
- 每个用例占一行 JSON，含 `name`、`meta`、可选的 `fixture` 和 `expect`。`meta` 是一个 `cabin.HoldMeta`。`fixture` 的内容去掉第一行后填进 `meta.host`。
- `expect` 含 `risk_level`、`suggest_allow` 和 `points`。`points` 是解释要点，只报告覆盖率，不参与门槛。
- 运行时每条用例使用独立的运行 ID。这绕开解释缓存和每次运行的调用次数上限。超时的用例换一个运行 ID 重试一次。仍失败才计为调用失败。
- 评分输出准确率、高风险召回率和误放行数。单条匹配要求风险等级和建议放行都正确。调用失败算不匹配。
- 误放行只统计期望高风险且建议放行的用例。
- 任一集合的高风险召回率低于 100%，或误放行数大于 0 时，命令以退出码 1 结束。没有可用密钥或装配失败时，以退出码 2 结束，并用中文说明原因。
- 模型调用经过一个专用闸门实例，使用占位符机制换真实密钥。报告不含占位符和密钥。
- 评估的设置写在临时目录里，不读写用户的 advisor 设置。
- `make eval` 运行 `go run ./tools/eval -root .`。它依次评估每一家有密钥的供应商，输出每一家的门槛判定。退出码取最差的一家。
- 只评估一家时，运行 `go run ./tools/eval -root . -provider anthropic` 或 `go run ./tools/eval -root . -provider deepseek`。
- 副驾提供给用户选择的每个供应商都要评估。没有达到门槛的供应商，副驾设置中显示中文说明。

## 6. 关键流程

### 6.1 启动运行

1. `console` 调用 `cabin.Start`。
2. `cabin` 读取预设，调用 `Inspector.Entry` 做进舱检查。
3. `cabin` 调用 `staging.Create`，把 `Inspector.Rewrite` 传给它。`Excluded` 的文件和 `Cached` 的目录不复制，改写清单中的文件改写后复制。复制时，`cabin` 发出 `progress` 事件。
4. `cabin` 打开本次运行的审计记录，生成终端令牌。
5. `cabin` 调用 `gate.Start`。`Host` 为 `airlock-gate:8080`，`Allow` 来自 `gate.TierHosts`，`Providers` 只含预设的供应商，`Secret` 为 `Options.Secret`。
6. 预设的界面为 web 时，`cabin` 在 `127.0.0.1` 上分配一个空闲端口作为 `HostPort`。
7. `cabin` 展开预设中的模板变量，调用 `Backend.Create`。`Volumes` 是 `Cached` 的目录，再加上项目根目录下的 `node_modules` 和 `.venv`。
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
   - 上一次请求没有返回时，运行页面不发新的请求。
   - 运行页面最多列出 200 个改动，并显示改动的总数。出舱审查页面列出全部改动。
2. 用户点击"查看内容"时，`console` 调用 `cabin.Diff`，`cabin` 调用 `Area.Diff`。出舱审查页面也提供这个按钮。
3. 用户点击"撤销这个改动"时，`console` 调用 `cabin.Revert`，`cabin` 调用 `Area.Revert`。
4. 用户在左侧的 agent 界面中告诉 agent 换一种做法。Airlock 不替用户向 agent 发送指令。

### 6.5 出舱审查

1. `console` 调用 `cabin.Stop`。`cabin` 删除沙箱，关闭闸门，让终端令牌失效，把状态改为 `Review`。
2. `console` 调用 `cabin.ReviewItems`。`cabin` 调用 `Area.Changes` 和 `Inspector.Exit`，合并结果。
3. 默认全部应用。`console` 为用户没有丢弃的每个改动调用 `cabin.Apply`，`cabin` 调用 `Area.Apply`。用户丢弃了文件时，界面提示改动可能不完整。
4. 用户完成审查时，`console` 调用 `cabin.FinishReview`。`cabin` 为 `ReviewItems` 中的每个改动写入一条 `audit.Review`，依赖目录的改动不记录。改动是否已应用，取自暂存区的写回记录，所以 Airlock 重启后报告仍然正确。之后 `cabin` 关闭审计记录，并调用 `Area.Discard`。`cabin` 把状态改为 `Idle`。
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
| `cabins\<id>\applied.json` | 已经写回项目的改动 | 否 |
| `airlock.lock` | 单实例锁 | 否 |
| `open-console` | 重新打开控制台的请求，内容为空 | 否 |
| `wizard-done` | 首次使用的向导已经完成 | 否 |

依赖缓存卷存放在 Docker Desktop 的虚拟机中，不在这个目录中。

启动时，`cmd/airlock` 检查写入这个目录的文件是否被 Windows 转存到打包应用的私有目录。发现转存时拒绝启动，原因见 [security.md](security.md#7-已知限制)。

之后，`cmd/airlock` 在调用 `cabin.New` 之前锁住数据目录。锁是 `airlock.lock` 的独占文件句柄，进程退出时系统自动释放。另一个进程已经持有锁时，这个进程不调用 `Prune`，也不修改 `cabins.json`。它请正在运行的实例重新打开控制台（#92）：

1. 这个进程创建空文件 `open-console`，最多等 3 秒。文件被删除时，它输出中文提示，以退出码 0 结束。
2. 正在运行的实例启动时先删除残留的 `open-console`，之后每秒删除一次这个文件。删除成功时，它用 `desktop.OpenURL` 再次打开带令牌的控制台网址。两次打开至少间隔 2 秒。间隔内的请求留在原处，间隔过后再处理，所以第二个进程报告成功时，浏览器确实打开了。
3. 3 秒内文件没有被删除时，这个进程删除文件，输出中文提示，以退出码 1 结束。

令牌只在正在运行的实例的内存中，不写入文件，也不经过第二个进程。

退出时（关闭窗口或按 Ctrl+C），`cmd/airlock` 按以下顺序清理：

1. 调用 `cabin.StopAll`，并发停止全部运行中的工作舱。这些工作舱进入 `Review`。
2. 最多等待 30 秒。Docker Desktop 不响应时，Airlock 仍然退出。没有停完的沙箱由下次启动时的 `Prune` 删除，`New` 把这些工作舱改为 `Review`。
   正在启动的工作舱不在 `StopAll` 停止的范围内。它的沙箱同样由下次启动时的 `Prune` 删除。
3. 关闭控制台、网页终端和副驾闸门，最后释放数据目录的锁。

关闭控制台窗口时，Windows 大约 5 秒后强制结束进程，所以这时的清理可能没有完成。

真实密钥只存于 Windows 凭据管理器，不在这个目录中。

## 8. 分工

| 成员 | 负责的层 | 负责的目录 |
|---|---|---|
| `lijiahao818` | 核心层、入口层 | `internal/cabin`、`internal/gate`、`internal/sandbox`、`internal/staging`、`internal/vault`、`internal/audit`、`internal/desktop`、`presets/`、`cmd/airlock` |
| `pionxe` | 界面层、扩展层 | `internal/console`、`internal/webterm`、`web/`、`internal/advisor`、`internal/filecheck`、`evals/`、`Makefile` |
