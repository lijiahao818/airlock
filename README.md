# Airlock 气闸舱

[![codecov](https://codecov.io/gh/lijiahao818/airlock/graph/badge.svg?token=9RLF79VUNI)](https://codecov.io/gh/lijiahao818/airlock)

Airlock 是一个面向桌面用户的 AI agent 安全沙箱。它在隔离环境中运行 Claude Code、Codex、DeepSeek Harness（`dsh`）等 agent。API key、密码等敏感信息只留在宿主机，不进入沙箱。

Airlock 在三个环节保护用户：

- 进舱：检查项目文件，不把密钥文件放进沙箱。
- 舱内：agent 的出网请求都经过闸门。陌生请求先暂扣，由用户决定。
- 出舱：agent 的改动先写入暂存区。用户审查后，改动才写入项目。

### 演示视频见 [Airlock 演示](https://www.vidline.com/video/V0WRH6N6BR)。

产品设计见 [docs/product.md](docs/product.md)。安全设计见 [docs/security.md](docs/security.md)。架构设计见 [docs/architecture.md](docs/architecture.md)。

## 前置条件

- Windows 10 或 Windows 11。密钥存放在 Windows 凭据管理器中，其他系统不能保存密钥。
- Docker Desktop。运行工作舱前，先启动 Docker Desktop。
- Go 1.26 或更高版本，用于构建程序。下载程序时不需要。
- Node.js 20.19 或更高版本，用于构建网页界面。下载程序时不需要。

## 下载

CI 用仓库源码构建程序，并发布到 GitHub Release。不想自己构建时，按下列步骤下载：

1. 打开 [Releases 页面](https://github.com/lijiahao818/airlock/releases)。
2. 下载最新版本中的 `airlock.exe` 和 `airlock.exe.sha256`。
3. 在 PowerShell 中计算程序的哈希值：

   ```powershell
   (Get-FileHash airlock.exe -Algorithm SHA256).Hash
   ```

4. 把输出与 `airlock.exe.sha256` 中的哈希值比对。不一致时，不要运行这个程序。

程序没有代码签名。第一次运行时，Windows 可能显示"Windows 已保护你的电脑"。点击"更多信息"，再点击"仍要运行"。

## 构建

1. 克隆仓库：

   ```bash
   git clone https://github.com/lijiahao818/airlock.git
   cd airlock
   ```

2. 构建控制台网页：

   ```bash
   npm ci --prefix web
   npm run build --prefix web
   ```

3. 构建网页终端：

   ```bash
   npm ci --prefix web/term
   npm run build --prefix web/term
   ```

4. 构建程序。程序会打包前两步的构建结果，所以按顺序执行：

   ```bash
   go build -o bin/airlock.exe ./cmd/airlock
   ```

## 运行

1. 启动 Docker Desktop。
2. 双击下载的 `airlock.exe` 或构建出的 `bin\airlock.exe`。也可以在终端中运行：

   ```bash
   ./bin/airlock.exe
   ```

3. 程序在浏览器中打开控制台。第一次使用时，按向导检查 Docker，了解安全副驾。
4. 在"密钥"页添加 agent 使用的 API key。使用 Claude Pro 或 Max 订阅时，在本机终端运行 `claude setup-token`，再把显示的订阅令牌添加到"密钥"页。
5. 在"新建工作舱"页选择项目文件夹和 agent（页面上叫"AI 助手"），创建工作舱。
6. 在控制台中启动工作舱。agent 运行时，运行页面显示 agent 的界面、出网动态和拦截卡片。
7. 停止工作舱后，在出舱审查页面选择要写回项目的改动，再查看运行报告。
8. 关闭 Airlock 的窗口，或按 Ctrl+C，退出程序。

关掉控制台的标签页或浏览器崩溃后，再次双击 `bin\airlock.exe`。Airlock 在浏览器中重新打开控制台，运行中的工作舱不受影响。

程序的数据存放在 `%LOCALAPPDATA%\Airlock`。API key 存放在 Windows 凭据管理器中。

不要在 Claude 桌面应用等打包应用的终端中启动 Airlock。在这些终端中，Windows 把 `%LOCALAPPDATA%` 中的新文件转存到该应用的私有目录，沙箱看不到项目文件。Airlock 在启动时检查这种情况，发现后拒绝启动。

查看程序的版本号：

```bash
./bin/airlock.exe version
```

## 沙箱中的语言环境

四个预设的沙箱中都装有 Node.js 24、Python 3（含标准库）和 git。Claude Code、Claude Code（订阅）和 Codex 的沙箱中还装有 tmux。沙箱中没有 Go、Rust、Java 等其他语言的环境。agent 也不能在沙箱中安装这些环境。

在"新建工作舱"页选择项目文件夹和 agent 后，Airlock 检查项目用的语言。项目用了沙箱中没有的语言时，页面显示提示。这时 agent 仍然可以修改代码，但不能在沙箱中运行和测试代码。

## 许可证

本项目使用 Apache-2.0 许可证。详见 [LICENSE](LICENSE)。
