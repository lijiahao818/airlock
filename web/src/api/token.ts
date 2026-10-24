// 控制台令牌放在 URL 片段 #token= 中，不发给服务器。见 docs/architecture.md 第 5.10 节。

// 令牌由 airlock 启动时生成。只接受 URL 安全的字符，长度与网页终端的令牌一致（见 5.11 节）。
const tokenRe = /^[A-Za-z0-9_-]{16,256}$/;

// parseToken 从片段字符串（含开头的 #）中取出令牌。没有令牌或格式不对时返回空字符串。
export function parseToken(hash: string): string {
  if (!hash.startsWith("#")) {
    return "";
  }
  const token = new URLSearchParams(hash.slice(1)).get("token") ?? "";
  return tokenRe.test(token) ? token : "";
}

// Location 是 loadToken 需要的地址栏部分。
export interface LocationLike {
  hash: string;
  pathname: string;
}

// History 把片段从地址栏中删掉的手段。
export interface HistoryLike {
  replace(path: string): void;
}

// loadToken 从地址栏读取令牌。读到后把片段从地址栏中删掉，令牌不再出现在地址栏里。
export function loadToken(loc: LocationLike, hist: HistoryLike): string {
  const token = parseToken(loc.hash);
  if (token !== "") {
    hist.replace(loc.pathname);
  }
  return token;
}

let token = "";

// setToken 记录令牌，供 API 客户端使用。
export function setToken(t: string): void {
  token = t;
}

// getToken 返回已记录的令牌。没有令牌时返回空字符串。
export function getToken(): string {
  return token;
}
