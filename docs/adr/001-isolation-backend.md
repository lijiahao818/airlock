# ADR-001 使用 Docker 容器作为隔离后端

## 状态

已接受。

## 背景

Airlock 运行在 Windows 上，目标电脑包括 Windows 家庭版。隔离后端要满足下列要求：

- agent 不能访问宿主机的文件和网络。
- 出网控制不依赖 agent 配合。
- 不需要 CGO。
- 团队能在比赛期限内完成。

## 决策

Airlock 使用 Docker Desktop 运行的 Linux 容器作为沙箱。

- Docker Desktop 在 Windows 上通过 WSL2 虚拟机运行容器。沙箱和 Windows 宿主机之间隔着这层虚拟机。
- Airlock 用下列参数启动沙箱：

  | 参数 | 作用 |
  |---|---|
  | 非 root 用户 | agent 没有管理员权限。 |
  | `--cap-drop ALL` | 删除全部 Linux 能力。 |
  | `--security-opt no-new-privileges` | 禁止进程提升权限。 |
  | `--read-only` 加临时目录 | 根文件系统只读。`/tmp` 允许执行文件，因为 DeepSeek Harness 要在 `/tmp` 中加载原生模块。 |
  | `--pids-limit`、`--memory` | 限制进程数和内存。 |
  | 只挂载暂存区 | 不挂载项目文件夹、home 目录和 Docker socket。 |

- 业务代码只通过 `sandbox.Backend` 接口使用 Docker。

## 备选方案

| 方案 | 不选的原因 |
|---|---|
| 专用 WSL2 发行版 | 全部 WSL2 发行版运行在同一个虚拟机中。发行版默认可以启动 Windows 程序，并把 C 盘挂载到 `/mnt/c`。关闭这些功能的配置文件 `/etc/wsl.conf` 在发行版内部，沙箱内的 root 用户可以改回。来源：[WSL 配置文档](https://learn.microsoft.com/en-us/windows/wsl/wsl-config)。 |
| Windows 受限令牌或 AppContainer | 只有进程级隔离。需要大量 Win32 API 开发，期限内无法完成。 |
| Windows Sandbox | 不支持 Windows 家庭版。默认联网。来源：[Windows Sandbox 文档](https://learn.microsoft.com/en-us/windows/security/application-security/application-isolation/windows-sandbox/windows-sandbox-overview)。 |
| 直接使用 Docker Sandboxes | Airlock 会变成 Docker Sandboxes 的外壳，失去产品差异。 |
| 自建 microVM | 期限内无法完成。 |

## 后果

- 好处：Docker 的 internal 网络强制执行出网隔离。容器加固参数成熟，并且已经验证，见 [security.md](../security.md#6-验证记录)。
- 坏处：用户必须安装 Docker Desktop。沙箱之间共享同一个 Linux 内核。
- 沙箱镜像必须使用基于 glibc 的 Linux 发行版。DeepSeek Harness 的原生模块在基于 musl 的 Alpine 上无法加载。
- `sandbox.Backend` 接口隔开了业务代码和隔离后端。更换隔离后端时，业务代码不变。
