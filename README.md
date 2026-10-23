# Airlock 气闸舱

Airlock 是一个桌面级 AI agent 安全沙箱。它在隔离环境中运行 Claude Code、Codex、dsh 等 agent。API key、密码等敏感信息只留在宿主机，不进入沙箱。

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
