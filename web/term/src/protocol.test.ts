import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { closeText, inputMessage, inputMessages, maxMessage, protocols, readParams, resizeMessage, wsURL } from "./protocol.ts";

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

// check 确认每条消息不超过上限、各段都是完整的 Unicode 字符，并返回各段的 input。
function check(data: string): string[] {
  const parts = inputMessages(data).map((m) => {
    assert.ok(Buffer.byteLength(m, "utf8") <= maxMessage, `消息有 ${Buffer.byteLength(m, "utf8")} 字节`);
    const p = JSON.parse(m) as { input: string };
    assert.deepEqual(Object.keys(p), ["input"]);
    assert.notEqual(p.input, "");
    return p.input;
  });
  assert.equal(parts.join(""), data);
  return parts;
}

// #57 AC-2、AC-3：各种字符的输入分段后都不超过上限，拼接后与原输入相同。
test("大段输入分成多条不超过上限的消息", () => {
  const cases: Record<string, string> = {
    ASCII: "a".repeat(200000),
    中文: "中".repeat(70000),
    emoji: "😀".repeat(40000),
    引号: '"'.repeat(70000),
    反斜杠: "\\".repeat(70000),
    控制字符: "\x00\x01\x1b\r\n\t\x7f".repeat(20000),
    混合: 'a中😀"\\\x1b[31m\x00'.repeat(10000),
  };
  for (const [name, data] of Object.entries(cases)) {
    const parts = check(data);
    assert.ok(parts.length > 1, name);
    // 各段是完整的码点：没有落单的代理项。
    for (const p of parts) {
      assert.equal(p, [...p].join(""), name);
      assert.ok(!/[\uD800-\uDBFF]$/.test(p) && !/^[\uDC00-\uDFFF]/.test(p), name);
    }
  }
});

// #57 AC-3：上限以内和恰好上限时是一条消息，超过上限时分成两条。
test("按序列化后的字节数分段", () => {
  const empty = inputMessage("").length;
  assert.deepEqual(inputMessages(""), []);
  assert.deepEqual(check("ls\r"), ["ls\r"]);
  const exact = "a".repeat(maxMessage - empty);
  assert.equal(Buffer.byteLength(inputMessage(exact)), maxMessage);
  assert.deepEqual(check(exact), [exact]);
  assert.deepEqual(check(exact + "b"), [exact, "b"]);
  // 转义后的 " 占 2 个字节，emoji 占 4 个字节。放不下时整个移到下一条，不拆开。
  const quotes = '"'.repeat((maxMessage - empty) / 2);
  assert.deepEqual(check(quotes), [quotes]);
  assert.deepEqual(check(quotes.slice(1) + "😀"), [quotes.slice(1), "😀"]);
});

// #57 AC-4：括号粘贴模式的开始和结束序列只出现一次，顺序不变。
test("分段后保留括号粘贴的开始和结束序列", () => {
  const start = "\x1b[200~";
  const end = "\x1b[201~";
  const data = start + "中\x1b".repeat(30000) + end;
  const joined = check(data).join("");
  assert.equal(joined.split(start).length, 2);
  assert.equal(joined.split(end).length, 2);
  assert.ok(joined.startsWith(start) && joined.endsWith(end));
});

// #57 AC-1：webterm 的 Go 测试把这份夹具中的消息发给网页终端，确认连接不断开、输入完整。
test("大段粘贴的消息与 webterm 测试使用的夹具相同", () => {
  const fixture = readFileSync(new URL("../../../internal/webterm/testdata/paste.json", import.meta.url), "utf8");
  assert.deepEqual(JSON.parse(fixture), inputMessages("中".repeat(22000)));
});

test("断开连接时告诉用户下一步", () => {
  for (const code of [1000, 1003, 1006, 1008, 1009]) {
    assert.match(closeText(code), /请/);
  }
});
