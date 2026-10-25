// 供应商和档位的显示名。名字来自 product.md 的 F-4 和 F-10。
const providerDisplayNames: Record<string, string> = {
  anthropic: "Anthropic",
  openai: "OpenAI",
  deepseek: "DeepSeek",
};

export function providerName(name: string): string {
  return providerDisplayNames[name] ?? name;
}

export interface TierInfo {
  name: string;
  desc: string;
}

const tierInfos: Record<string, TierInfo> = {
  strict: {
    name: "严格",
    desc: "只自动放行模型接口，其余出网都要经过你同意",
  },
  standard: {
    name: "标准",
    desc: "模型接口和常用包管理源自动放行，其余经过你同意",
  },
  permissive: {
    name: "宽松",
    desc: "模型接口、包管理源和常用开发网站自动放行",
  },
};

export function tierInfo(tier: string): TierInfo {
  return tierInfos[tier] ?? { name: tier, desc: "" };
}

// 工作舱状态的显示名（cabin.Status）。
const statusNames: Record<string, string> = {
  idle: "未运行",
  running: "运行中",
  review: "待审查",
};

export function statusName(s: string): string {
  return statusNames[s] ?? s;
}

// 风险等级的显示名。等级来自暂扣解释（docs/architecture.md 第 5.7 节）。
const riskLevelNames: Record<string, string> = {
  low: "低",
  medium: "中",
  high: "高",
};

export function riskLevelName(level: string): string {
  return riskLevelNames[level] ?? level;
}

// 出网规则的显示名。规则取值见 docs/architecture.md 第 5.1 节。
const ruleNames: Record<string, string> = {
  route: "模型接口",
  tier: "安全档位",
  "run-allow": "本次运行已允许",
  "run-deny": "本次运行已拒绝",
  "user-allow": "你允许的",
  "user-deny": "你拒绝的",
  timeout: "超时未决定",
  "private-address": "内网地址",
  "bad-host": "可疑主机",
  "bad-placeholder": "占位符问题",
  "route-path": "不允许的模型接口",
  "no-key": "没有密钥",
  unsupported: "不支持的请求",
  busy: "同时连接太多",
  hold: "等待你的决定",
};

export function ruleName(rule: string): string {
  return ruleNames[rule] ?? rule;
}

// 改动类型的显示名（staging.Kind）。
const changeKindNames: Record<string, string> = {
  added: "新增",
  modified: "修改",
  deleted: "删除",
};

export function changeKindName(kind: string): string {
  return changeKindNames[kind] ?? kind;
}

// 进舱检查和改动发现的显示名（cabin.FindingKind）。
const findingKindNames: Record<string, string> = {
  excluded: "不进沙箱",
  cached: "依赖缓存",
  notice: "需要注意",
  "high-risk": "高风险",
};

export function findingKindName(kind: string): string {
  return findingKindNames[kind] ?? kind;
}

// 语言环境的显示名。取值与预设的 languages 相同（docs/architecture.md 第 5.6 节）。
const languageNames: Record<string, string> = {
  node: "Node.js",
  python: "Python",
  go: "Go",
  rust: "Rust",
  java: "Java",
  c: "C",
  cpp: "C++",
};

export function languageName(lang: string): string {
  return languageNames[lang] ?? lang;
}

// specialChangeText 是不能写回项目的改动的提示。运行页和出舱审查页面使用同一句话。
export const specialChangeText =
  "这是符号链接、特殊文件，或者文件名以点或空格结尾（Windows 不能正确读写），不能写回项目，将丢弃。";
