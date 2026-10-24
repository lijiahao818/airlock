import { test } from "node:test";
import assert from "node:assert/strict";
import { loadToken, parseToken } from "./token.ts";

const token = "fake_abcdefghijklmnopqrstuvwxyz0123456789-_";

test("从片段中读取令牌", () => {
  assert.equal(parseToken(`#token=${token}`), token);
  assert.equal(parseToken(`#a=1&token=${token}`), token);
});

test("片段里没有可用的令牌时返回空字符串", () => {
  for (const hash of [
    "",
    "#",
    "#token=",
    "#token=short",
    `#token=${token}%20x`,
    `#x=${token}`,
    `token=${token}`,
    `#token=${token},evil`,
  ]) {
    assert.equal(parseToken(hash), "", hash);
  }
});

test("loadToken 读到令牌后把片段从地址栏中删掉", () => {
  const replaced: string[] = [];
  const t = loadToken(
    { hash: `#token=${token}`, pathname: "/", search: "" },
    { replace: (p) => replaced.push(p) },
  );
  assert.equal(t, token);
  assert.deepEqual(replaced, ["/"]);
});

test("删掉片段时保留查询参数", () => {
  const replaced: string[] = [];
  const t = loadToken(
    { hash: `#token=${token}`, pathname: "/run", search: "?cabin=a&start=1" },
    { replace: (p) => replaced.push(p) },
  );
  assert.equal(t, token);
  assert.deepEqual(replaced, ["/run?cabin=a&start=1"]);
});

test("没有令牌时 loadToken 不动地址栏", () => {
  const replaced: string[] = [];
  const t = loadToken(
    { hash: "", pathname: "/keys", search: "" },
    { replace: (p) => replaced.push(p) },
  );
  assert.equal(t, "");
  assert.deepEqual(replaced, []);
});
