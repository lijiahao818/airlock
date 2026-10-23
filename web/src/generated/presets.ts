// 本文件由 go run ./tools/tsgen 生成，不要手动修改。
// 来源：presets

// Preset 对应一个 agent 目录中的 preset.json。值中可以使用的模板变量见
// docs/architecture.md 第 5.6 节。
export interface Preset {
  id: string;
  name: string;
  provider: string; // gate.Providers() 中的一个
  ui: string; // terminal 或 web
  cmd: string[]; // terminal：在终端中运行的命令。web：容器的主进程
  env?: Record<string, string>; // 值中可以使用模板变量
  files?: Record<string, string>; // 沙箱内的路径 → 内容，可以使用模板变量
  web?: Web; // UI 为 web 时必填
}

export interface Web {
  port: number; // 沙箱内网页服务的端口
  tokenPattern: string; // 从启动输出中提取令牌的正则表达式，必须有一个分组
  openPath: string; // 网页界面的路径，例如 /?token={token}
}
