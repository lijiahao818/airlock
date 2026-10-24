// 事件流的读取和解析。后端用 text/event-stream 输出运行事件（internal/console/events.go）。
// 架构约定用 fetch 读取事件流，这样请求能带上 X-Airlock-Token 请求头；EventSource 做不到。

import type * as cabin from "../generated/cabin";
import { ApiError } from "./client.ts";
import { getToken } from "./token.ts";

// createSSEParser 逐块接收响应文本，返回刚好完整到达的事件数据。
// 一条 SSE 消息由若干行组成，空行结束。只有 data: 开头的行携带数据，
// 多行 data 按 SSE 规范用换行拼接，其余行直接忽略。
// 后端只输出 \n 换行，这里不处理 \r\n。
export function createSSEParser(): { push(chunk: string): string[] } {
  let buf = "";
  return {
    push(chunk: string): string[] {
      buf += chunk;
      const out: string[] = [];
      for (;;) {
        const end = buf.indexOf("\n\n");
        if (end < 0) {
          break;
        }
        const raw = buf.slice(0, end);
        buf = buf.slice(end + 2);
        const data = raw
          .split("\n")
          .filter((line) => line.startsWith("data:"))
          .map((line) => line.replace(/^data: ?/, ""))
          .join("\n");
        if (data !== "") {
          out.push(data);
        }
      }
      return out;
    },
  };
}

// deliver 把一条事件的 JSON 交给回调。解析失败的数据不是事件，跳过这一条。
function deliver(onEvent: (e: cabin.Event) => void, data: string): void {
  let e: cabin.Event;
  try {
    e = JSON.parse(data) as cabin.Event;
  } catch {
    return;
  }
  onEvent(e);
}

// followEvents 持续读取 /api/events，把每条事件交给 onEvent，直到 signal 中止。
// 连接断开或服务器暂时不可用时，等 retryDelayMs 毫秒后重连。
// 请求被拒绝（4xx，比如令牌不对）时抛 ApiError，由页面显示，不重试。
export async function followEvents(
  onEvent: (e: cabin.Event) => void,
  signal: AbortSignal,
  retryDelayMs = 2000,
): Promise<void> {
  for (;;) {
    try {
      const resp = await fetch("/api/events", {
        headers: { "X-Airlock-Token": getToken() },
        signal,
      });
      if (!resp.ok) {
        const text = (await resp.text()).trim();
        if (resp.status >= 400 && resp.status < 500) {
          throw new ApiError(
            resp.status,
            text === "" ? "事件流连接被拒绝。请刷新页面后重试。" : text,
          );
        }
        throw new Error(`HTTP ${resp.status}`);
      }
      if (resp.body === null) {
        throw new Error("响应没有内容");
      }
      const reader = resp.body.getReader();
      const parser = createSSEParser();
      const dec = new TextDecoder();
      for (;;) {
        const { done, value } = await reader.read();
        if (done) {
          break;
        }
        for (const data of parser.push(dec.decode(value, { stream: true }))) {
          deliver(onEvent, data);
        }
      }
    } catch (err) {
      if (signal.aborted) {
        return;
      }
      if (err instanceof ApiError) {
        throw err;
      }
      // 网络问题或服务器重启，走重连。
    }
    if (signal.aborted) {
      return;
    }
    await new Promise<void>((resolve) => {
      const timer = setTimeout(resolve, retryDelayMs);
      signal.addEventListener(
        "abort",
        () => {
          clearTimeout(timer);
          resolve();
        },
        { once: true },
      );
    });
    if (signal.aborted) {
      return;
    }
  }
}
