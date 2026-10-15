export const blankRootBudget = {
  autoRetry: false,
  maxSeconds: 0,
  maxModelCalls: 0,
  maxToolCalls: 0,
  maxTokens: 0,
  maxPlannerRounds: 0,
  maxAttempts: 0,
  maxRetries: 0,
};
export type ModelDraft = {
  provider: string;
  baseUrl: string;
  name: string;
};
export const remoteProviders = ["openai", "anthropic", "gemini"] as const;
export const providerDefaults: Record<string, string> = {
  ollama: "http://127.0.0.1:11434/v1",
  lmstudio: "http://127.0.0.1:1234/v1",
  openai: "https://api.openai.com/v1",
  anthropic: "https://api.anthropic.com",
  gemini: "https://generativelanguage.googleapis.com",
};
export const providerLabels: Record<string, string> = {
  ollama: "Ollama（本机）",
  lmstudio: "LM Studio（本机）",
  openai: "OpenAI",
  anthropic: "Anthropic",
  gemini: "Gemini",
};
export const scopeLabels: Record<string, string> = {
  public: "公开内容",
  workspace: "你允许访问的文件",
  personal: "聊天内容",
  credential: "凭据",
  unknown: "来源尚未分类的资料",
};
