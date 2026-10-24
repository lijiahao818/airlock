import { test } from "node:test";
import assert from "node:assert/strict";
import { closeText, inputMessage, protocols, readParams, resizeMessage, wsURL } from "./protocol.ts";

const token = "fake_abcdefghijklmnopqrstuvwxyz0123456789-_";

test("从地址中读取工作舱和令牌", () => {
  assert.deepEqual(readParams(`http://127.0.0.1:9000/?cabin=c1#token=${token}`), { cabin: "c1", token });
});

test("缺少工作舱或令牌格式不对时返回 null", () => {
  for (const href of [
    `http://127.0.0.1:9000/#token=${token}`,
    "http://127.0.0.1:9000/?cabin=c1",
    "http://127.0.0.1:9000/?cabin=c1#token=short",
    `http://127.0.0.1:9000/?cabin=c1#token=${token},evil`,
    `http://127.0.0.1:9000/?cabin=c1#token=${token}%20x`,
    // 令牌只从片段中读取，放在查询参数中会被发给服务器。
    `http://127.0.0.1:9000/?cabin=c1&token=${token}`,
  ]) {
    assert.equal(readParams(href), null, href);
  }
});

test("连接同源的 /ws", () => {
  assert.equal(wsURL("http://127.0.0.1:9000/?cabin=c1", "c1"), "ws://127.0.0.1:9000/ws?cabin=c1");
  assert.equal(wsURL("https://127.0.0.1:9000/", "a&b"), "wss://127.0.0.1:9000/ws?cabin=a%26b");
});

test("子协议是 airlock 和令牌", () => {
  assert.deepEqual(protocols(token), ["airlock", token]);
});

test("消息格式与 webterm 一致", () => {
  assert.equal(inputMessage('ls "a"\r'), '{"input":"ls \\"a\\"\\r"}');
  assert.equal(resizeMessage(120, 40), '{"cols":120,"rows":40}');
});

test("断开连接时告诉用户下一步", () => {
  for (const code of [1000, 1003, 1006, 1008, 1009]) {
    assert.match(closeText(code), /请/);
  }
});
