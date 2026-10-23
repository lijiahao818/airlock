// 本文件由 go run ./tools/tsgen 生成，不要手动修改。
// 来源：internal/gate

// Event 是一次出网判定的元数据。不含请求体、请求头和查询参数。
export interface Event {
  time: string;
  host: string;
  port: number;
  method: string; // CONNECT，或模型路由上的 HTTP 方法
  path?: string; // 只有模型路由有路径，不含查询参数
  rule: string; // 命中的规则，取值见 docs/architecture.md 第 5.1 节
  allowed: boolean;
  provider?: string; // 换上了真实密钥时为供应商名，否则为空
}
