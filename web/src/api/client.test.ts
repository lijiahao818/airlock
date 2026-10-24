import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { ApiError, request } from "./client.ts";
import { setToken } from "./token.ts";

const token = "fake_abcdefghijklmnopqrstuvwxyz0123456789-_";

// Call 记录一次 fetch 收到的参数。
interface Call {
  path: string;
  method: string;
  headers: Record<string, string>;
  body?: string;
}

// stubFetch 把全局 fetch 换成返回固定响应的假实现，并记录每次调用。
function stubFetch(status: number, body: string): Call[] {
  const calls: Call[] = [];
  globalThis.fetch = (async (path: string, init?: { method?: string; body?: string; headers?: Record<string, string> }) => {
    calls.push({
      path: String(path),
      method: init?.method ?? "GET",
      headers: { ...(init?.headers ?? {}) },
      body: init?.body,
    });
    return new Response(body, { status });
  }) as typeof fetch;
  return calls;
}

beforeEach(() => {
  setToken(token);
});

test("每个请求都带 X-Airlock-Token", async () => {
  const calls = stubFetch(200, "[]");
  const list = await request<string[]>("/api/cabins");
  assert.deepEqual(list, []);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].path, "/api/cabins");
  assert.equal(calls[0].headers["X-Airlock-Token"], token);
});

test("有请求体时带 JSON 的类型头", async () => {
  const calls = stubFetch(200, "{}");
  await request("/api/keys/anthropic", { method: "PUT", body: '{"secret":"fake-sk-1234"}' });
  assert.equal(calls[0].headers["Content-Type"], "application/json");
  assert.equal(calls[0].body, '{"secret":"fake-sk-1234"}');
});

test("没有令牌时不发请求，用中文告诉用户下一步", async () => {
  const calls = stubFetch(200, "{}");
  setToken("");
  await assert.rejects(
    () => request("/api/cabins"),
    (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.equal((err as ApiError).status, 401);
      assert.match((err as ApiError).message, /请/);
      return true;
    },
  );
  assert.equal(calls.length, 0);
});

test("服务器拒绝时把中文说明带给用户", async () => {
  stubFetch(401, "控制台令牌缺失或不正确。请从 Airlock 启动时打开的页面进入。");
  await assert.rejects(
    () => request("/api/cabins"),
    (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.equal((err as ApiError).status, 401);
      assert.equal((err as ApiError).message, "控制台令牌缺失或不正确。请从 Airlock 启动时打开的页面进入。");
      return true;
    },
  );
});

test("服务器没有说明时给出重试的下一步", async () => {
  stubFetch(500, "");
  await assert.rejects(
    () => request("/api/cabins"),
    (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.match((err as ApiError).message, /HTTP 500.*重试/);
      return true;
    },
  );
});

test("成功的空响应体按没有数据处理", async () => {
  stubFetch(200, "");
  const result = await request<void>("/api/cabins/c1/start", { method: "POST" });
  assert.equal(result, undefined);
});
