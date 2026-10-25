import { test } from "node:test";
import assert from "node:assert/strict";
import { activityFromReport } from "./activity.ts";

test("用报告重建出网动态：次数、暂扣次数和最后一次决定", () => {
  const list = activityFromReport({
    hosts: [
      { host: "fake-a.example", allowed: 2, denied: 1 },
      { host: "fake-b.example", allowed: 0, denied: 1 },
      { host: "registry.npmjs.org", allowed: 5, denied: 0 },
    ],
    holds: [
      { host: "fake-a.example", rule: "timeout" },
      { host: "fake-a.example", rule: "user-allow" },
      { host: "fake-b.example", rule: "user-deny" },
    ],
    keyUses: {},
    files: [],
  });
  assert.deepEqual(list, [
    { host: "fake-a.example", allowed: 2, denied: 1, holds: 2, decision: "user-allow" },
    { host: "fake-b.example", allowed: 0, denied: 1, holds: 1, decision: "user-deny" },
    { host: "registry.npmjs.org", allowed: 5, denied: 0, holds: 0, decision: "" },
  ]);
});

test("没有记录时返回空列表", () => {
  assert.deepEqual(activityFromReport({ hosts: [], holds: [], keyUses: {}, files: [] }), []);
});
