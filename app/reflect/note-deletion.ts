import { isEmptyNewNoteDraft, UNTITLED_NOTE_TITLE } from "./note-drafts";
import type { DocumentRecord } from "./types";

export function noteDeletionConfirmation(documents: readonly Pick<DocumentRecord, "title" | "content">[]): string | null {
  if (documents.length === 0) return null;
  if (documents.length === 1) {
    const document = documents[0];
    if (isEmptyNewNoteDraft({ title: UNTITLED_NOTE_TITLE, content: document.content })) return null;
    return `确定删除“${document.title.trim() || UNTITLED_NOTE_TITLE}”吗？\n\n笔记会移到废纸篓，可恢复。`;
  }
  return `确定删除选中的 ${documents.length} 篇笔记吗？\n\n笔记会移到废纸篓，可恢复。`;
}
