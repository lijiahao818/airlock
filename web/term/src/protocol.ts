// 网页终端与 webterm 之间的约定，见 docs/architecture.md 第 5.11 节。

export interface Params {
  cabin: string;
  token: string;
}

// 令牌放在 Sec-WebSocket-Protocol 中，只允许 URL 安全的 base64 字符。
const tokenRe = /^[A-Za-z0-9_-]{16,256}$/;

// readParams 从地址中读取工作舱 ID 和令牌。令牌在片段 #token= 中，不发给服务器。
export function readParams(href: string): Params | null {
  const u = new URL(href);
  const cabin = u.searchParams.get("cabin") ?? "";
  const token = new URLSearchParams(u.hash.slice(1)).get("token") ?? "";
  if (cabin === "" || !tokenRe.test(token)) {
    return null;
  }
  return { cabin, token };
}

// wsURL 返回同源的 /ws 地址。
export function wsURL(href: string, cabin: string): string {
  const u = new URL(href);
  const scheme = u.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${u.host}/ws?cabin=${encodeURIComponent(cabin)}`;
}

// protocols 返回握手时提供的子协议：airlock 和令牌。
export function protocols(token: string): string[] {
  return ["airlock", token];
}

export function inputMessage(data: string): string {
  return JSON.stringify({ input: data });
}

export function resizeMessage(cols: number, rows: number): string {
  return JSON.stringify({ cols, rows });
}

// closeText 用大白话说明连接为什么断开，以及用户可以怎么做。
export function closeText(code: number): string {
  switch (code) {
    case 1000:
      return "agent 已经退出。请回到控制台查看运行状态。";
    case 1003:
    case 1008:
    case 1009:
      return "网页终端发送的数据格式不对，连接已断开。请刷新页面重新连接。";
    default:
      return "和 agent 的连接断开了。agent 仍在运行，请刷新页面重新连接。如果仍然连不上，请从控制台重新打开运行页面。";
  }
}
