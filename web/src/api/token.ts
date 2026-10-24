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
  search: string;
}

// History 把片段从地址栏中删掉的手段。
export interface HistoryLike {
  replace(path: string): void;
}

// SessionStore 读写浏览器的会话存储。单元测试传入内存实现，不依赖浏览器。
export interface SessionStore {
  get(): string;
  set(token: string): void;
}

const sessionKey = "airlock-token";

// 浏览器的会话存储。禁用会话存储时读写失败，令牌退回只存在内存里，刷新后要重新进入。
const browserSession: SessionStore = {
  get() {
    try {
      return window.sessionStorage.getItem(sessionKey) ?? "";
    } catch {
      return "";
    }
  },
  set(token: string) {
    try {
      window.sessionStorage.setItem(sessionKey, token);
    } catch {
      // 存不进去时不影响本次使用。
    }
  },
};

// loadToken 从地址栏读取令牌。读到后把片段从地址栏中删掉，令牌不再出现在地址栏里，
// 同时写入会话存储，这样刷新运行页面（product.md F-6）后不用重新运行 airlock。
// 查询参数不属于片段，原样保留。
// 片段里没有令牌时，从会话存储恢复上次读到的令牌。会话存储只在本标签页有效，关闭标签页自动清除。
export function loadToken(
  loc: LocationLike,
  hist: HistoryLike,
  session: SessionStore = browserSession,
): string {
  const token = parseToken(loc.hash);
  if (token !== "") {
    session.set(token);
    hist.replace(loc.pathname + loc.search);
    return token;
  }
  const saved = session.get();
  return tokenRe.test(saved) ? saved : "";
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
