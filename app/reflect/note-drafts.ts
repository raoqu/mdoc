import { parseFrontmatter, splitFrontmatter } from "./frontmatter";
import { documentsInNotebook, updateFolders, type DocumentRecord, type FolderRecord, type NotebookRecord } from "./types";

export const UNTITLED_NOTE_TITLE = "未命名笔记";

export function isEmptyNewNoteDraft(document: Pick<DocumentRecord, "title" | "content">): boolean {
  const title = document.title.trim();
  if (title && title !== UNTITLED_NOTE_TITLE) return false;
  const { body, raw } = splitFrontmatter(document.content);
  const frontmatter = parseFrontmatter(raw);
  // Explicit frontmatter titles and invalid YAML are user input worth keeping.
  if (frontmatter.warning || (typeof frontmatter.data.title === "string" && frontmatter.data.title.trim())) return false;
  const content = body
    .replace(/\r\n/g, "\n")
    .replace(/^[ \t]*#{1,6}[ \t]*$/gm, "")
    .trim();
  return content === "" || /^[-+*]$/.test(content);
}

// Only suppress new drafts created in this session; saved notes stay in the payload.
export function notebooksWithSavableDrafts(
  notebooks: readonly NotebookRecord[],
  newDraftIds: ReadonlySet<string>,
): NotebookRecord[] {
  return notebooks.map((notebook) => ({
    ...notebook,
    folders: updateFolders(notebook.folders, (folder) => ({
      ...folder,
      docs: folder.docs.filter((document) => !newDraftIds.has(document.id) || !isEmptyNewNoteDraft(document)),
    })),
  }));
}

export function mergeNewNoteDrafts(
  saved: readonly NotebookRecord[],
  current: readonly NotebookRecord[],
  newDraftIds: ReadonlySet<string>,
): NotebookRecord[] {
  const savedIds = new Set(saved.flatMap((notebook) => documentsInNotebook(notebook).map(({ document }) => document.id)));
  const mergeFolders = (remote: readonly FolderRecord[], local: readonly FolderRecord[]): FolderRecord[] => {
    const result = [...remote];
    for (const folder of local) {
      const index = result.findIndex((candidate) => candidate.id === folder.id);
      const existing = result[index];
      const drafts = folder.docs.filter((document) => newDraftIds.has(document.id) && !savedIds.has(document.id));
      const children = mergeFolders(existing?.children ?? [], folder.children);
      if (existing) {
        result[index] = { ...existing, docs: [...existing.docs, ...drafts], children };
      } else if (drafts.length > 0 || children.length > 0) {
        result.push({ ...folder, docs: drafts, children });
      }
    }
    return result;
  };
  return saved.map((notebook) => {
    const local = current.find((candidate) => candidate.id === notebook.id);
    return local ? { ...notebook, folders: mergeFolders(notebook.folders, local.folders) } : notebook;
  });
}
