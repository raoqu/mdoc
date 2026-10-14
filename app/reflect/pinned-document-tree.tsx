"use client";

import { useState, type DragEvent, type KeyboardEvent } from "react";
import { ChevronRight, GripVertical, PinOff } from "lucide-react";
import { normalizePinnedTree, type TreePlacement } from "./document-tree";
import type { DocumentLocation, DocumentTreeNode, NotebookRecord } from "./types";

interface Props {
  notebook: NotebookRecord;
  documents: readonly DocumentLocation[];
  activeId?: string;
  disabled?: boolean;
  onOpen: (id: string) => void;
  onUnpin: (id: string) => void;
  onMove: (id: string, target: string | null, placement: TreePlacement) => void;
}

const dragType = "application/x-mdoc-pinned-document";

export function PinnedDocumentTree({ notebook, documents, activeId, disabled, onOpen, onUnpin, onMove }: Props) {
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [dragged, setDragged] = useState<string | null>(null);
  const [drop, setDrop] = useState<{ id: string; placement: TreePlacement } | null>(null);
  const nodes = normalizePinnedTree(notebook);
  const titles = new Map(documents.map(({ document }) => [document.id, document.title]));

  const placementAt = (event: DragEvent<HTMLElement>): TreePlacement => {
    const bounds = event.currentTarget.getBoundingClientRect();
    const ratio = (event.clientY - bounds.top) / bounds.height;
    return ratio < 0.25 ? "before" : ratio > 0.75 ? "after" : "inside";
  };

  const keyboardMove = (event: KeyboardEvent, node: DocumentTreeNode, siblings: readonly DocumentTreeNode[], index: number, parent: DocumentTreeNode | null) => {
    if (!event.altKey || disabled) return;
    if (event.key === "ArrowUp" && index > 0) onMove(node.id, siblings[index - 1].id, "before");
    else if (event.key === "ArrowDown" && index < siblings.length - 1) onMove(node.id, siblings[index + 1].id, "after");
    else if (event.key === "ArrowRight" && index > 0) onMove(node.id, siblings[index - 1].id, "inside");
    else if (event.key === "ArrowLeft" && parent) onMove(node.id, parent.id, "after");
    else return;
    event.preventDefault();
    event.stopPropagation();
  };

  const render = (items: readonly DocumentTreeNode[], parent: DocumentTreeNode | null = null) => <ul>
    {items.map((node, index) => <li key={node.id}>
      <div className={`pinned-tree-row ${activeId === node.id ? "active" : ""}`} data-drop={drop?.id === node.id ? drop.placement : undefined}
        draggable={!disabled}
        onDragStart={(event) => { event.stopPropagation(); event.dataTransfer.setData(dragType, node.id); event.dataTransfer.effectAllowed = "move"; setDragged(node.id); }}
        onDragEnd={() => { setDragged(null); setDrop(null); }}
        onDragOver={(event) => { if (disabled || !dragged) return; event.preventDefault(); event.stopPropagation(); event.dataTransfer.dropEffect = "move"; setDrop({ id: node.id, placement: placementAt(event) }); }}
        onDragLeave={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node)) setDrop(null); }}
        onDrop={(event) => { event.preventDefault(); event.stopPropagation(); const source = event.dataTransfer.getData(dragType); if (source && !disabled) { onMove(source, node.id, placementAt(event)); if (placementAt(event) === "inside") setCollapsed((current) => { const next = new Set(current); next.delete(node.id); return next; }); } setDragged(null); setDrop(null); }}>
        <GripVertical size={12} className="pinned-tree-grip" aria-hidden="true" />
        {node.children?.length ? <button type="button" className="pinned-tree-toggle" aria-expanded={!collapsed.has(node.id)} aria-label={`${collapsed.has(node.id) ? "展开" : "收起"} ${titles.get(node.id)}`} onClick={() => setCollapsed((current) => { const next = new Set(current); if (!next.delete(node.id)) next.add(node.id); return next; })}><ChevronRight size={13} className={collapsed.has(node.id) ? "" : "expanded"} /></button> : <span className="pinned-tree-toggle" />}
        <button type="button" className="pinned-tree-link" aria-current={activeId === node.id ? "page" : undefined} onClick={() => onOpen(node.id)} onKeyDown={(event) => keyboardMove(event, node, items, index, parent)} title="Alt + ↑↓ 排序，Alt + → 嵌套，Alt + ← 提升层级">{titles.get(node.id) || "未命名笔记"}</button>
        <button type="button" className="pinned-tree-unpin" disabled={disabled} aria-label={`取消固定 ${titles.get(node.id)}`} title="取消固定；子节点提升到同级" onClick={() => onUnpin(node.id)}><PinOff size={12} /></button>
      </div>
      {node.children?.length && !collapsed.has(node.id) ? render(node.children, node) : null}
    </li>)}
  </ul>;

  if (nodes.length === 0) return null;
  return <section className="sidebar-section pinned-document-tree" aria-label="固定文档目录">
    <h3>已固定</h3>
    <p className="pinned-tree-hint">拖拽排序或嵌套 · Alt + 方向键调整</p>
    {render(nodes)}
    {dragged ? <div className="pinned-tree-root-drop" onDragOver={(event) => { if (!disabled) event.preventDefault(); }} onDrop={(event) => { event.preventDefault(); if (!disabled) onMove(event.dataTransfer.getData(dragType), null, "inside"); setDragged(null); setDrop(null); }}>移到顶层末尾</div> : null}
  </section>;
}
