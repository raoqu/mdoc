import { Document, isMap, parseDocument } from "yaml";

export interface FrontmatterSplit {
  raw: string | null;
  header: string;
  body: string;
  bodyOffset: number;
}

export interface ParsedFrontmatter {
  data: Record<string, unknown>;
  warning?: string;
}

const OPEN_FENCE = /^---[ \t]*\r?\n/;
const CLOSE_FENCE = /(?:^|\r?\n)---[ \t]*(?:\r?\n|$)/;

export function splitFrontmatter(source: string): FrontmatterSplit {
  const open = OPEN_FENCE.exec(source);
  if (!open || open.index !== 0) {
    return { raw: null, header: "", body: source, bodyOffset: 0 };
  }
  const afterOpen = open[0].length;
  const rest = source.slice(afterOpen);
  const close = CLOSE_FENCE.exec(rest);
  if (!close) {
    return { raw: null, header: "", body: source, bodyOffset: 0 };
  }
  const raw = rest.slice(0, close.index);
  const bodyOffset = afterOpen + close.index + close[0].length;
  return {
    raw,
    header: source.slice(0, bodyOffset),
    body: source.slice(bodyOffset),
    bodyOffset,
  };
}

export function parseFrontmatter(raw: string | null): ParsedFrontmatter {
  if (raw === null || raw.trim() === "") {
    return { data: {} };
  }
  try {
    const document = parseDocument(raw);
    if (document.errors.length > 0) {
      return {
        data: {},
        warning: `YAML frontmatter 无效：${document.errors[0].message}`,
      };
    }
    if (!isMap(document.contents)) {
      return { data: {}, warning: "frontmatter 不是键值映射，已忽略" };
    }
    return { data: document.toJS() as Record<string, unknown> };
  } catch (cause) {
    return {
      data: {},
      warning: `YAML frontmatter 无效：${
        cause instanceof Error ? cause.message : String(cause)
      }`,
    };
  }
}

export function joinFrontmatter(header: string, body: string): string {
  return `${header}${body}`;
}

export function upsertFrontmatter(
  source: string,
  patch: Record<string, unknown>,
): string {
  if (Object.keys(patch).length === 0) {
    return source;
  }
  const { raw, body } = splitFrontmatter(source);
  if (raw === null) {
    const defined = Object.fromEntries(
      Object.entries(patch).filter(([, value]) => value !== undefined),
    );
    if (Object.keys(defined).length === 0) {
      return source;
    }
    const document = new Document(defined);
    return `---\n${String(document)}---\n${body}`;
  }

  const document = parseDocument(raw);
  if (!isMap(document.contents)) {
    return source;
  }
  for (const [key, value] of Object.entries(patch)) {
    if (value === undefined) {
      document.delete(key);
    } else {
      document.set(key, value);
    }
  }
  if (document.contents.items.length === 0) {
    return body;
  }
  return `---\n${String(document)}---\n${body}`;
}

export function aliasesFromFrontmatter(source: string): string[] {
  const parsed = parseFrontmatter(splitFrontmatter(source).raw).data;
  const aliases = parsed.aliases;
  if (!Array.isArray(aliases)) {
    return [];
  }
  return aliases.filter((value): value is string => typeof value === "string");
}

export function privateFromFrontmatter(source: string): boolean {
  return parseFrontmatter(splitFrontmatter(source).raw).data.private === true;
}

