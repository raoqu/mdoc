export interface ClipboardTextContent {
  markdown?: string;
  plainText?: string;
  html?: string;
}

export type ClipboardInsertion =
  | { kind: "markdown" | "plain-text"; text: string }
  | { kind: "rich-text"; html: string };

const MARKDOWN_BLOCK_PATTERN =
  /(^|\n)(?: {0,3}(?:#{1,6}\s|>\s|[-+*]\s+(?:\[[ xX]\]\s+)?|\d+[.)]\s+|`{3,}|~{3,})|\|?\s*:?-{3,}:?\s*\|)/m;
const MARKDOWN_INLINE_PATTERN =
  /(?:!\[[^\]\n]*\]\([^\n)]+\)|\[[^\]\n]+\]\([^\n)]+\)|\[\[[^\]\n]+\]\]|`[^`\n]+`|\*\*[^*\n]+\*\*|__[^_\n]+__|~~[^~\n]+~~)/;

/**
 * Detects syntax that is unlikely to occur in ordinary rich-text clipboard
 * fallbacks. A positive match lets Markdown source win when both text/plain
 * and text/html are present on the clipboard.
 */
export function looksLikeMarkdown(text: string): boolean {
  const normalized = text.replaceAll(/\r\n?/g, "\n");
  return (
    MARKDOWN_BLOCK_PATTERN.test(normalized) ||
    MARKDOWN_INLINE_PATTERN.test(normalized)
  );
}

export function chooseClipboardInsertion(
  content: ClipboardTextContent,
): ClipboardInsertion | undefined {
  if (content.markdown?.trim()) {
    return { kind: "markdown", text: content.markdown };
  }

  if (content.plainText?.trim() && looksLikeMarkdown(content.plainText)) {
    return { kind: "markdown", text: content.plainText };
  }

  if (content.html?.trim()) {
    return { kind: "rich-text", html: content.html };
  }

  if (content.plainText?.trim()) {
    return { kind: "plain-text", text: content.plainText };
  }

  return undefined;
}

async function textForType(
  items: readonly ClipboardItem[],
  type: string,
): Promise<string | undefined> {
  for (const item of items) {
    if (!item.types.includes(type)) continue;
    return (await item.getType(type)).text();
  }
  return undefined;
}

export async function readClipboardTextContent(): Promise<ClipboardTextContent> {
  if (!navigator.clipboard) {
    throw new Error("当前浏览器不支持读取剪贴板");
  }

  if (typeof navigator.clipboard.read === "function") {
    const items = await navigator.clipboard.read();
    const markdownType = ["text/markdown", "text/x-markdown"].find((type) =>
      items.some((item) => item.types.includes(type)),
    );
    const [markdown, plainText, html] = await Promise.all([
      markdownType ? textForType(items, markdownType) : undefined,
      textForType(items, "text/plain"),
      textForType(items, "text/html"),
    ]);
    return { markdown, plainText, html };
  }

  if (typeof navigator.clipboard.readText === "function") {
    return { plainText: await navigator.clipboard.readText() };
  }

  throw new Error("当前浏览器不支持读取剪贴板");
}
