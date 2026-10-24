import { test } from "node:test";
import assert from "node:assert/strict";
import { createSSEParser, followEvents } from "./events.ts";
import { setToken } from "./token.ts";
import type * as cabin from "../generated/cabin";

test("解析完整的 data 行", () => {
  const p = createSSEParser();
  assert.deepEqual(p.push('data: {"a":1}\n\n'), ['{"a":1}']);
  assert.deepEqual(p.push(""), []);
});

test("跨块到达的事件拼接后再输出", () => {
  const p = createSSEParser();
  assert.deepEqual(p.push('data: {"a"'), []);
  assert.deepEqual(p.push(':1}\n'), []);
  assert.deepEqual(p.push("\n"), ['{"a":1}']);
});

test("一个块里的多条事件全部输出", () => {
  const p = createSSEParser();
  assert.deepEqual(p.push("data: a\n\ndata: b\n\n"), ["a", "b"]);
});

test("多行 data 用换行拼接，非 data 行忽略", () => {
  const p = createSSEParser();
  assert.deepEqual(p.push(": 注释\ndata: 第一行\ndata: 第二行\nid: 7\n\n"), ["第一行\n第二行"]);
});

test("只有非 data 行的事件不输出", () => {
  const p = createSSEParser();
  assert.deepEqual(p.push(": 注释\n\n"), []);
});

test("followEvents 解析流中的事件，跳过坏数据，断开后停止", async () => {
  const enc = new TextEncoder();
  const status: cabin.Event = { kind: "status", cabin: "a", status: "running" };
  const progress: cabin.Event = { kind: "progress", cabin: "a", progress: { done: 1, total: 2 } };
  const first = new ReadableStream<Uint8Array>({
    start(c) {
      c.enqueue(enc.encode(`data: ${JSON.stringify(status)}\n\n`));
      c.enqueue(enc.encode("data: 不是 JSON\n\n"));
      c.enqueue(enc.encode(`data: ${JSON.stringify(progress)}\n\n`));
      c.close();
    },
  });
  // 第二次连接不返回响应，等中止信号到来再拒绝，模拟一条停着的连接。
  let calls = 0;
  const origFetch = globalThis.fetch;
  globalThis.fetch = ((_input: unknown, init?: { signal?: AbortSignal }) => {
    calls += 1;
    if (calls === 1) {
      return Promise.resolve(new Response(first));
    }
    return new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
    });
  }) as typeof fetch;
  setToken("fake_abcdefghijklmnop");
  const ac = new AbortController();
  const events: cabin.Event[] = [];
  const pending = followEvents((e) => events.push(e), ac.signal, 10);
  const timer = setTimeout(() => ac.abort(), 50);
  await pending;
  clearTimeout(timer);
  globalThis.fetch = origFetch;
  assert.equal(calls, 2);
  assert.equal(events.length, 2);
  assert.equal(events[0].status, "running");
  assert.equal(events[1].progress?.done, 1);
});

test("followEvents 被拒绝时抛 ApiError 且不重试", async () => {
  const origFetch = globalThis.fetch;
  globalThis.fetch = (async () => new Response("缺少令牌", { status: 401 })) as typeof fetch;
  try {
    await assert.rejects(
      followEvents(() => {}, new AbortController().signal),
      /缺少令牌/,
    );
  } finally {
    globalThis.fetch = origFetch;
  }
});
