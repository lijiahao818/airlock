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
