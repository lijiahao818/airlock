# Airlock 气闸舱

Airlock 是一个面向桌面用户的 AI agent 安全沙箱。它在隔离环境中运行 Claude Code、Codex、DeepSeek Harness（`dsh`）等 agent。API key、密码等敏感信息只留在宿主机，不进入沙箱。

Airlock 在三个环节保护用户：

- 进舱：检查项目文件，不把密钥文件放进沙箱。
- 舱内：agent 的出网请求都经过闸门。陌生请求先暂扣，由用户决定。
- 出舱：agent 的改动先写入暂存区。用户审查后，改动才写入项目。

产品设计见 [docs/product.md](docs/product.md)。安全设计见 [docs/security.md](docs/security.md)。架构设计见 [docs/architecture.md](docs/architecture.md)。

## 安装

构建需要 Go 1.26 或更高版本。

1. 克隆仓库：

   ```bash
   git clone https://github.com/lijiahao818/airlock.git
   cd airlock
   ```

2. 构建程序：

   ```bash
   go build -o bin/airlock ./cmd/airlock
   ```

## 运行

运行以下命令，程序输出版本号：

```bash
./bin/airlock version
```

## 许可证

本项目使用 Apache-2.0 许可证。详见 [LICENSE](LICENSE)。
