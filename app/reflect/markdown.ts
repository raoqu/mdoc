import type { DocumentLocation } from "./types";

export interface WikiLink {
  target: string;
  index: number;
}

export interface MarkdownTask {
  id: string;
  documentId: string;
  documentTitle: string;
  line: number;
  content: string;
  checked: boolean;
  dueDate: string | null;
  breadcrumbs: string[];
}

const WIKI_LINK_PATTERN = /\[\[([^\]\n|#]+)(?:[|#][^\]\n]*)?\]\]/g;
const TAG_PATTERN = /(^|[\s(])#([\p{L}\p{N}_/-]+)/gu;
const TASK_PATTERN = /^(\s*)\+\s+\[([ xX])\]\s+(.*)$/;
const DUE_DATE_PATTERN = /\[\[(\d{4}-\d{2}-\d{2})\]\]/;

export function titleFromMarkdown(markdown: string, fallback: string): string {
  const match = markdown.match(/^#\s+(.+)$/m);
  return match?.[1]?.replace(/[*_`]/g, "").trim() || fallback;
}

export function wikiLinksIn(markdown: string): WikiLink[] {
  return Array.from(markdown.matchAll(WIKI_LINK_PATTERN), (match) => ({
    target: match[1].trim(),
    index: match.index ?? 0,
  }));
}

export function renameWikiLinks(
  markdown: string,
  from: string,
  to: string,
): string {
  if (/[[\]|\r\n]/.test(to)) {
    throw new Error("Wiki 链接标题不能包含 [、]、| 或换行");
  }
  const fromKey = from.trim().toLocaleLowerCase();
  let fenced = false;
  return markdown
    .split("\n")
    .map((line) => {
      if (/^\s*(```|~~~)/.test(line)) {
        fenced = !fenced;
        return line;
      }
      if (fenced) {
        return line;
      }
      return line.replace(
        /\[\[([^\]\n|#]+)(#[^\]\n|]+)?(?:\|([^\]\n]+))?\]\]/g,
        (whole, target: string, fragment: string | undefined, alias: string | undefined) => {
          if (target.trim().toLocaleLowerCase() !== fromKey) {
            return whole;
          }
          const suffix = fragment ?? "";
          return alias
            ? `[[${to}${suffix}|${alias}]]`
            : `[[${to}${suffix}]]`;
        },
      );
    })
    .join("\n");
}

export function tagsIn(markdown: string): string[] {
  return Array.from(markdown.matchAll(TAG_PATTERN), (match) => match[2].toLocaleLowerCase());
}

export function allTags(documents: readonly DocumentLocation[]): string[] {
  return Array.from(
    new Set(documents.flatMap(({ document }) => tagsIn(document.content))),
  ).sort((left, right) => left.localeCompare(right, "zh-CN"));
}

export function backlinksFor(
  target: DocumentLocation,
  documents: readonly DocumentLocation[],
): DocumentLocation[] {
  const keys = new Set(
    [target.document.title, ...(target.document.aliases ?? [])].map((value) =>
      value.trim().toLocaleLowerCase(),
    ),
  );
  return documents.filter(
    ({ document }) =>
      document.id !== target.document.id &&
      wikiLinksIn(document.content).some((link) =>
        keys.has(link.target.toLocaleLowerCase()),
      ),
  );
}

export function tasksIn(documents: readonly DocumentLocation[]): MarkdownTask[] {
  return documents.flatMap(({ document }) => {
    const lines = document.content.split("\n");
    const ancestors: { indent: number; label: string }[] = [];
    return lines.flatMap((line, lineIndex) => {
      const listItem = line.match(/^(\s*)[-+*]\s+(?:\[[ xX]\]\s+)?(.*)$/);
      const indent = listItem?.[1].replaceAll("\t", "    ").length ?? -1;
      if (listItem) {
        while (ancestors.length > 0 && ancestors.at(-1)!.indent >= indent) {
          ancestors.pop();
        }
      }
      const match = line.match(TASK_PATTERN);
      if (!match) {
        if (listItem && listItem[2].trim()) {
          ancestors.push({ indent, label: listItem[2].trim() });
        }
        return [];
      }
      const content = match[3];
      const explicitDate = content.match(DUE_DATE_PATTERN)?.[1] ?? null;
      const dailyDate = document.id.match(/^daily-(\d{4}-\d{2}-\d{2})$/)?.[1] ?? null;
      const breadcrumbs = ancestors
        .filter((item) => item.indent < indent)
        .map((item) => item.label)
        .filter((label) => !/^(tasks?|todos?)\s*:?$/i.test(label));
      ancestors.push({ indent, label: content });
      return [
        {
          id: `${document.id}:${lineIndex}`,
          documentId: document.id,
          documentTitle: document.title,
          line: lineIndex,
          content,
          checked: match[2].toLocaleLowerCase() === "x",
          dueDate: explicitDate ?? dailyDate,
          breadcrumbs,
        },
      ];
    });
  });
}

export function toggleTask(
  markdown: string,
  lineIndex: number,
  expectedContent?: string,
): string {
  const lines = markdown.split("\n");
  const line = lines[lineIndex];
  if (line === undefined) {
    return markdown;
  }
  const match = line.match(TASK_PATTERN);
  if (!match) {
    throw new Error("任务已发生变化，请刷新后重试");
  }
  if (expectedContent !== undefined && match[3] !== expectedContent) {
    throw new Error("任务已发生变化，请刷新后重试");
  }
  lines[lineIndex] = `${match[1]}+ [${match[2] === " " ? "x" : " "}] ${match[3]}`;
  return lines.join("\n");
}

export function rescheduleTask(
  markdown: string,
  lineIndex: number,
  expectedContent: string,
  date: string | null,
): string {
  const lines = markdown.split("\n");
  const line = lines[lineIndex];
  const match = line?.match(TASK_PATTERN);
  if (!match || match[3] !== expectedContent) {
    throw new Error("任务已发生变化，请刷新后重试");
  }
  const withoutDate = match[3].replace(DUE_DATE_PATTERN, "").replace(/\s{2,}/g, " ").trimEnd();
  const content = date ? `${withoutDate} [[${date}]]` : withoutDate;
  lines[lineIndex] = `${match[1]}+ [${match[2]}] ${content}`;
  return lines.join("\n");
}

export type ConflictResolution = "ours" | "theirs" | "both";

export function hasConflictMarkers(markdown: string): boolean {
  return /<<<<<<< [^\n]*\n[\s\S]*?\n=======\n[\s\S]*?\n>>>>>>> [^\n]*(?:\n|$)/.test(markdown);
}

export function resolveConflictMarkers(
  markdown: string,
  resolution: ConflictResolution,
): string {
  const pattern =
    /<<<<<<< [^\n]*\n([\s\S]*?)\n=======\n([\s\S]*?)\n>>>>>>> [^\n]*(\n|$)/g;
  if (!pattern.test(markdown)) {
    throw new Error("没有可解决的同步冲突");
  }
  pattern.lastIndex = 0;
  return markdown.replace(pattern, (_whole, ours: string, theirs: string, ending: string) => {
    const selected =
      resolution === "ours"
        ? ours
        : resolution === "theirs"
          ? theirs
          : `${ours}\n${theirs}`;
    return selected + ending;
  });
}

export function excerpt(markdown: string, query = "", length = 150): string {
  const plain = markdown
    .replace(/^---[\s\S]*?---\s*/m, "")
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/[#>*_`[\]()!+|-]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  if (!query) {
    return plain.slice(0, length);
  }
  const index = plain.toLocaleLowerCase().indexOf(query.toLocaleLowerCase());
  const start = Math.max(0, index - 45);
  return `${start > 0 ? "…" : ""}${plain.slice(start, start + length)}`;
}

export function formatDailyTitle(date: string): string {
  const value = new Date(`${date}T12:00:00`);
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "long",
    day: "numeric",
    weekday: "long",
  }).format(value);
}

export function localDateKey(date = new Date()): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}
