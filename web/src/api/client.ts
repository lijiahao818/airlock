import { getToken } from "./token.ts";

// ApiError 是请求没有成功时抛出的错误。message 是给用户看的中文说明。
export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

const noTokenText =
  "没有从网址中读到控制台令牌。请关掉这个页面，从 Airlock 启动时打开的页面进入。";

// errText 把请求抛出的错误转成给用户看的中文说明。
export function errText(err: unknown): string {
  if (err instanceof ApiError) {
    return err.message;
  }
  return "操作没有完成，网络可能出了问题。请稍后重试。";
}

// Body 是请求体。控制台的请求体都是小的 JSON。
export interface RequestOptions {
  method?: string;
  body?: string;
}

// request 发一个 /api 请求，自动带上 X-Airlock-Token。
// 服务器拒绝时抛 ApiError，message 用服务器返回的中文说明。
export async function request<T>(path: string, init?: RequestOptions): Promise<T> {
  const token = getToken();
  if (token === "") {
    throw new ApiError(401, noTokenText);
  }
  const headers: Record<string, string> = { "X-Airlock-Token": token };
  if (init?.body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  const resp = await fetch(path, { method: init?.method ?? "GET", body: init?.body, headers });
  if (!resp.ok) {
    const text = (await resp.text()).trim();
    throw new ApiError(
      resp.status,
      text === "" ? `请求没有成功（HTTP ${resp.status}）。请刷新页面后重试。` : text,
    );
  }
  return (await resp.json()) as T;
}
