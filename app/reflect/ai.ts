export type AiProviderId = "openai" | "anthropic" | "google" | "openrouter";

export interface AiProviderConfig {
  id: string;
  provider: AiProviderId;
  label: string;
  model: string;
  keyHint: string;
  baseUrl?: string;
  isDefault: boolean;
  createdAt: string;
}

export interface AiPrompt {
  id: string;
  label: string;
  body: string;
  mode: "replace" | "append";
}

export const AI_PROVIDER_CATALOG = [
  {
    id: "openai" as const,
    label: "OpenAI",
    keyPlaceholder: "sk-…",
    models: ["gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.4", "gpt-5.4-mini"],
  },
  {
    id: "anthropic" as const,
    label: "Anthropic",
    keyPlaceholder: "sk-ant-…",
    models: ["claude-fable-5", "claude-opus-4-8", "claude-sonnet-5", "claude-sonnet-4-6"],
  },
  {
    id: "google" as const,
    label: "Google Gemini",
    keyPlaceholder: "AIza…",
    models: ["gemini-3.1-pro-preview", "gemini-3.5-flash", "gemini-2.5-pro"],
  },
  {
    id: "openrouter" as const,
    label: "OpenRouter",
    keyPlaceholder: "sk-or-v1-…",
    models: ["openrouter/auto", "~openai/gpt-latest", "~anthropic/claude-sonnet-latest"],
  },
] as const;

const FILLER =
  "不要给结果加引号，不要翻译原文。保留原始 Markdown 格式，包括 [[双链]] 和 #标签。只返回处理后的文本。";

export const BUILT_IN_AI_PROMPTS: readonly AiPrompt[] = [
  {
    id: "fix-grammar",
    label: "修正拼写和语法",
    body: `修正下面文本中的拼写、语法和标点；已经正确的部分不要改动。\n\n{{selectedText}}\n\n${FILLER}`,
    mode: "replace",
  },
  {
    id: "copy-editor",
    label: "作为文字编辑润色",
    body: `润色下面文本的可读性、行文节奏和段落结构，同时保持原意。\n\n{{selectedText}}\n\n${FILLER}`,
    mode: "replace",
  },
  {
    id: "rephrase",
    label: "换一种表达",
    body: `用不同措辞重写下面文本，含义保持不变。\n\n{{selectedText}}\n\n${FILLER}`,
    mode: "replace",
  },
  {
    id: "simplify",
    label: "简化并压缩",
    body: `简化并压缩下面文本，删除重复和空泛表达。\n\n{{selectedText}}\n\n${FILLER}`,
    mode: "replace",
  },
  {
    id: "format-paragraphs",
    label: "整理段落",
    body: `把下面文本整理成大小适当、层次清楚的段落。\n\n{{selectedText}}\n\n${FILLER}`,
    mode: "replace",
  },
  {
    id: "summary",
    label: "写一段简短摘要",
    body: `用简洁直白的语言概括下面文本。\n\n{{selectedText}}\n\n${FILLER}`,
    mode: "append",
  },
  {
    id: "takeaways",
    label: "列出关键要点",
    body: `从下面文本中提取至少三个关键要点，输出 Markdown 无序列表。\n\n{{selectedText}}\n\n${FILLER}`,
    mode: "append",
  },
  {
    id: "actions",
    label: "提取行动项",
    body: `只提取下面文本真正暗示的行动，输出 Markdown 待办列表（- [ ]）。\n\n{{selectedText}}\n\n${FILLER}`,
    mode: "append",
  },
  {
    id: "document",
    label: "将要点整理成文档",
    body: `把下面要点整理成简明、结构清晰的 Markdown 文档，重点论点使用粗体。\n\n{{selectedText}}\n\n${FILLER}`,
    mode: "replace",
  },
  {
    id: "continue",
    label: "续写下一段",
    body: `沿用下面文本的语言、语气与主题续写下一段，至少三句，不重复已有内容。\n\n{{selectedText}}\n\n${FILLER}`,
    mode: "append",
  },
  {
    id: "backlinks",
    label: "为实体添加双链",
    body: `保留原文，为其中的人名、公司、地点和项目添加 [[双链]]，不要给动作或动词加链接。\n\n{{selectedText}}\n\n${FILLER}`,
    mode: "replace",
  },
];

export interface AiStreamEvent {
  type: "start" | "text-delta" | "complete" | "error";
  text?: string;
  message?: string;
  conversationId?: string;
  sources?: { id: string; title: string }[];
}

export async function consumeEventStream(
  response: Response,
  onEvent: (event: AiStreamEvent) => void,
): Promise<void> {
  if (!response.ok) {
    throw new Error((await response.text()) || `请求失败（${response.status}）`);
  }
  if (!response.body) {
    throw new Error("当前环境不支持流式响应");
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  while (true) {
    const { done, value } = await reader.read();
    buffer += decoder.decode(value, { stream: !done }).replaceAll("\r\n", "\n");
    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      const block = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      const data = block
        .split("\n")
        .filter((line) => line.startsWith("data:"))
        .map((line) => line.slice(5).trimStart())
        .join("\n");
      if (data) {
        onEvent(JSON.parse(data) as AiStreamEvent);
      }
      boundary = buffer.indexOf("\n\n");
    }
    if (done) break;
  }
}
