# Airlock 气闸舱

Airlock 是一个面向桌面用户的 AI agent 安全沙箱。它在隔离环境中运行 Claude Code、Codex、DeepSeek Harness（`dsh`）等 agent。API key、密码等敏感信息只留在宿主机，不进入沙箱。

Airlock 在三个环节保护用户：

- 进舱：检查项目文件，不把密钥文件放进沙箱。
- 舱内：agent 的出网请求都经过闸门。陌生请求先暂扣，由用户决定。
- 出舱：agent 的改动先写入暂存区。用户审查后，改动才写入项目。

产品设计见 [docs/product.md](docs/product.md)。安全设计见 [docs/security.md](docs/security.md)。架构设计见 [docs/architecture.md](docs/architecture.md)。

## 前置条件

- Windows 10 或 Windows 11。密钥存放在 Windows 凭据管理器中，其他系统不能保存密钥。
- Docker Desktop。运行工作舱前，先启动 Docker Desktop。
- Go 1.26 或更高版本，用于构建程序。
- Node.js 20.19 或更高版本，用于构建网页界面。

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
2. 双击 `bin\airlock.exe`，或在终端中运行：

   ```bash
   ./bin/airlock.exe
   ```

3. 程序在浏览器中打开控制台。第一次使用时，按向导添加 API key、选择项目文件夹和 agent。
4. 在控制台中启动工作舱。agent 运行时，运行页面显示 agent 的界面、出网动态和拦截卡片。
5. 停止工作舱后，在出舱审查页面选择要写回项目的改动，再查看运行报告。
6. 关闭 Airlock 的窗口，或按 Ctrl+C，退出程序。

程序的数据存放在 `%LOCALAPPDATA%\Airlock`。API key 存放在 Windows 凭据管理器中。

不要在 Claude 桌面应用等打包应用的终端中启动 Airlock。在这些终端中，Windows 把 `%LOCALAPPDATA%` 中的新文件转存到该应用的私有目录，沙箱看不到项目文件。Airlock 在启动时检查这种情况，发现后拒绝启动。

运行以下命令，程序输出版本号：

```bash
./bin/airlock.exe version
```

## 许可证

本项目使用 Apache-2.0 许可证。详见 [LICENSE](LICENSE)。
