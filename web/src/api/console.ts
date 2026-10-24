// 控制台 HTTP API 的前端封装。路由见 docs/architecture.md 第 5.10 节。
// 数据类型从 src/generated/ 导入。请求和响应的外壳类型与 internal/console
// 中同名结构一致，修改任何一边时一起改。

import type * as advisor from "../generated/advisor";
import type * as cabin from "../generated/cabin";
import type * as presets from "../generated/presets";
import { request } from "./client.ts";

// 对应 internal/console/api.go 的 providerInfo。
export interface ProviderInfo {
  name: string;
  hint: string; // 密钥的后 4 位。没有保存密钥时为空。
}

// 对应 internal/console/cabins.go 的 createReq。
export interface CreateCabinReq {
  project: string;
  preset: string;
  tier: string;
}

// 对应 internal/console/api.go 的 pathResp。
export interface PathResp {
  path: string; // 用户取消选择时为空
}

export function checkDocker(): Promise<void> {
  return request<void>("/api/docker");
}

export function listProviders(): Promise<ProviderInfo[]> {
  return request<ProviderInfo[]>("/api/providers");
}

export function setKey(provider: string, secret: string): Promise<void> {
  return request<void>(`/api/keys/${encodeURIComponent(provider)}`, {
    method: "PUT",
    body: JSON.stringify({ secret }),
  });
}

export function deleteKey(provider: string): Promise<void> {
  return request<void>(`/api/keys/${encodeURIComponent(provider)}`, { method: "DELETE" });
}

export function testKey(provider: string): Promise<void> {
  return request<void>(`/api/keys/${encodeURIComponent(provider)}/test`, { method: "POST" });
}

export function listPresets(): Promise<presets.Preset[]> {
  return request<presets.Preset[]>("/api/presets");
}

export function listTiers(): Promise<string[]> {
  return request<string[]>("/api/tiers");
}

export function getAdvisor(): Promise<advisor.Settings> {
  return request<advisor.Settings>("/api/advisor");
}

export function setAdvisor(s: advisor.Settings): Promise<void> {
  return request<void>("/api/advisor", { method: "PUT", body: JSON.stringify(s) });
}

export function pickFolder(): Promise<PathResp> {
  return request<PathResp>("/api/pick-folder", { method: "POST" });
}

export function listCabins(): Promise<cabin.Cabin[]> {
  return request<cabin.Cabin[]>("/api/cabins");
}

export function createCabin(req: CreateCabinReq): Promise<cabin.Cabin> {
  return request<cabin.Cabin>("/api/cabins", { method: "POST", body: JSON.stringify(req) });
}

export function startCabin(id: string): Promise<void> {
  return request<void>(`/api/cabins/${encodeURIComponent(id)}/start`, { method: "POST" });
}

export function stopCabin(id: string): Promise<void> {
  return request<void>(`/api/cabins/${encodeURIComponent(id)}/stop`, { method: "POST" });
}

export function resetCabin(id: string): Promise<void> {
  return request<void>(`/api/cabins/${encodeURIComponent(id)}/reset`, { method: "POST" });
}
