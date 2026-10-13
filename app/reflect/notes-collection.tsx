"use client";

import { useMemo, useState } from "react";
import { FilePlus2, Pin, PinOff, Trash2, X } from "lucide-react";
import { excerpt } from "./markdown";
import type { DocumentLocation } from "./types";

interface NotesCollectionProps {
  title: string;
  documents: readonly DocumentLocation[];
  onNewNote: () => void;
  onOpenDocument: (documentId: string) => void;
  onPinDocuments: (documentIds: readonly string[], pinned: boolean) => void;
  onTrashDocuments: (documentIds: readonly string[]) => Promise<boolean>;
}

export function NotesCollection({ title, documents, onNewNote, onOpenDocument, onPinDocuments, onTrashDocuments }: NotesCollectionProps) {
  const [selectedIds, setSelectedIds] = useState(new Set<string>());
  const rows = useMemo(() => documents.filter(({ document }) => !document.trashed).sort(
    (left, right) => new Date(right.document.updatedAt).getTime() - new Date(left.document.updatedAt).getTime(),
  ), [documents]);
  const selected = rows.filter(({ document }) => selectedIds.has(document.id));
  const allSelected = rows.length > 0 && selected.length === rows.length;
  const partiallySelected = selected.length > 0 && !allSelected;
  const selectedDocumentIds = selected.map(({ document }) => document.id);
  const toggleSelected = (documentId: string) => setSelectedIds((current) => {
    const next = new Set(current);
    if (next.has(documentId)) next.delete(documentId);
    else next.add(documentId);
    return next;
  });
  const trash = async (documentIds: readonly string[]) => {
    if (await onTrashDocuments(documentIds)) {
      setSelectedIds((current) => new Set([...current].filter((id) => !documentIds.includes(id))));
    }
  };

  return (
    <section className="collection-screen">
      <header className="collection-header">
        <div><h1>{title}</h1><p>{rows.length} 篇笔记</p></div>
        <button type="button" className="primary-action" onClick={onNewNote}><FilePlus2 size={16} /> 新建笔记</button>
      </header>
      {selected.length > 0 ? (
        <div className="notes-bulk-toolbar" role="toolbar" aria-label="已选笔记操作">
          <strong>已选择 {selected.length} 篇</strong>
          <button type="button" disabled={selected.every(({ document }) => document.pinned)} onClick={() => onPinDocuments(selectedDocumentIds, true)}><Pin size={15} />固定</button>
          <button type="button" disabled={selected.every(({ document }) => !document.pinned)} onClick={() => onPinDocuments(selectedDocumentIds, false)}><PinOff size={15} />取消固定</button>
          <button type="button" className="danger" title="移到废纸篓" onClick={() => void trash(selectedDocumentIds)}><Trash2 size={15} />删除</button>
          <button type="button" className="notes-clear-selection" aria-label="取消选择" title="取消选择" onClick={() => setSelectedIds(new Set())}><X size={16} /></button>
        </div>
      ) : null}
      <div className="notes-table" role="table" aria-label={title}>
        <div className="notes-row notes-heading" role="row">
          <div role="columnheader" className="notes-selection-cell">
            <input type="checkbox" aria-label="全选笔记" aria-checked={partiallySelected ? "mixed" : allSelected}
              checked={allSelected} disabled={rows.length === 0}
              ref={(input) => { if (input) input.indeterminate = partiallySelected; }}
              onChange={() => setSelectedIds(allSelected ? new Set() : new Set(rows.map(({ document }) => document.id)))} />
          </div>
          <span role="columnheader">标题</span>
          <span role="columnheader" className="notes-location">位置</span>
          <span role="columnheader" className="notes-updated">更新时间</span>
          <span role="columnheader" className="notes-actions-heading">操作</span>
        </div>
        {rows.map(({ document, folder }) => (
          <div className={`notes-row ${selectedIds.has(document.id) ? "selected" : ""}`} role="row" key={document.id} data-document-id={document.id}>
            <div role="cell" className="notes-selection-cell">
              <input type="checkbox" aria-label={`选择笔记：${document.title}`} checked={selectedIds.has(document.id)} onChange={() => toggleSelected(document.id)} />
            </div>
            <div role="cell" className="notes-title-cell">
              <button type="button" className="notes-open-button" aria-label={`打开笔记：${document.title}`} onClick={() => onOpenDocument(document.id)}>
                <span className="notes-title">{document.pinned ? <Pin size={13} aria-hidden="true" /> : null}<strong>{document.title}</strong></span>
                <small>{excerpt(document.content, "", 100)}</small>
              </button>
            </div>
            <span role="cell" className="notes-location">{folder.title}</span>
            <span role="cell" className="notes-updated">{new Date(document.updatedAt).toLocaleDateString("zh-CN")}</span>
            <div role="cell" className="notes-row-actions">
              <button type="button" aria-label={`${document.pinned ? "取消固定" : "固定"}笔记：${document.title}`} title={document.pinned ? "取消固定" : "固定"} onClick={() => onPinDocuments([document.id], !document.pinned)}>
                {document.pinned ? <PinOff size={15} /> : <Pin size={15} />}
              </button>
              <button type="button" className="danger" aria-label={`删除笔记：${document.title}`} title="删除（移到废纸篓）" onClick={() => void trash([document.id])}><Trash2 size={15} /></button>
            </div>
          </div>
        ))}
      </div>
      {rows.length === 0 ? <p className="notes-empty-copy">还没有笔记，点击“新建笔记”开始记录。</p> : null}
    </section>
  );
}
