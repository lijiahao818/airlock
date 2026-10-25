import { test } from "node:test";
import assert from "node:assert/strict";
import { effectiveProvider, suggestionLabel } from "./advisor.ts";

test("两家都有密钥、没有选择时默认用 Anthropic", () => {
  assert.equal(effectiveProvider("", true, true), "anthropic");
  assert.equal(effectiveProvider("deepseek", true, true), "deepseek");
  assert.equal(effectiveProvider("anthropic", true, true), "anthropic");
});

test("只有一家密钥时用这一家，选择不起作用", () => {
  assert.equal(effectiveProvider("deepseek", true, false), "anthropic");
  assert.equal(effectiveProvider("", false, true), "deepseek");
  assert.equal(effectiveProvider("anthropic", false, false), "");
});

test("规则模板的建议不写副驾建议", () => {
  assert.equal(suggestionLabel("ai"), "副驾建议");
  assert.equal(suggestionLabel("template"), "规则的默认建议");
  assert.ok(!suggestionLabel("template").includes("副驾"));
});
