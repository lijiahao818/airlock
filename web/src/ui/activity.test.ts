import { test } from "node:test";
import assert from "node:assert/strict";
import { activityFromReport, hostPort } from "./activity.ts";

test("用报告重建出网动态：次数、暂扣次数和最后一次决定", () => {
  const list = activityFromReport({
    hosts: [
      { host: "fake-a.example", port: 443, allowed: 2, denied: 1 },
      { host: "fake-b.example", port: 443, allowed: 0, denied: 1 },
      { host: "registry.npmjs.org", port: 443, allowed: 5, denied: 0 },
      { host: "fake-a.example", port: 22, allowed: 0, denied: 1 },
    ],
    holds: [
      { host: "fake-a.example", port: 443, rule: "timeout" },
      { host: "fake-a.example", port: 443, rule: "user-allow" },
      { host: "fake-b.example", port: 443, rule: "user-deny" },
      { host: "fake-a.example", port: 22, rule: "user-deny" },
    ],
    keyUses: {},
    files: [],
  });
  assert.deepEqual(list, [
    { host: "fake-a.example:443", allowed: 2, denied: 1, holds: 2, decision: "user-allow" },
    { host: "fake-b.example:443", allowed: 0, denied: 1, holds: 1, decision: "user-deny" },
    { host: "registry.npmjs.org:443", allowed: 5, denied: 0, holds: 0, decision: "" },
    // 同一主机的不同端口分开显示，决定也分开（#73）。
    { host: "fake-a.example:22", allowed: 0, denied: 1, holds: 1, decision: "user-deny" },
  ]);
});

test("主机和端口的写法与闸门一致", () => {
  assert.equal(hostPort("example.com", 443), "example.com:443");
  assert.equal(hostPort("2606:4700::1111", 443), "[2606:4700::1111]:443");
});

test("没有记录时返回空列表", () => {
  assert.deepEqual(activityFromReport({ hosts: [], holds: [], keyUses: {}, files: [] }), []);
});
