// 本文件由 go run ./tools/tsgen 生成，不要手动修改。
// 来源：internal/console

// ProviderInfo 是 get /api/providers 列表中的一项。前端类型由 tsgen 生成。
export interface ProviderInfo {
  name: string;
  hint: string; // 密钥的后 4 位。没有保存密钥时为空。
}

// SecretReq 是 put /api/keys/{provider} 的请求体。
export interface SecretReq {
  secret: string;
}

// PathResp 是 post /api/pick-folder 的响应。
export interface PathResp {
  path: string;
}

// CreateReq 是 post /api/cabins 的请求体。前端类型由 tsgen 生成。
export interface CreateReq {
  project: string;
  preset: string;
  tier: string;
}

// PathReq 是改动类接口的请求体。
export interface PathReq {
  path: string;
}

// HostReq 是 reset-decision 的请求体。
export interface HostReq {
  host: string;
}

// AllowReq 是拦截决定的请求体。
export interface AllowReq {
  allow: boolean;
}

// URLResp 是 get /api/cabins/{id}/agent-url 的响应。
export interface URLResp {
  url: string;
}
