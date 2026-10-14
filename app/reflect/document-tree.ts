import { documentsInNotebook, type DocumentTreeNode, type NotebookRecord } from "./types";

export type TreePlacement = "before" | "after" | "inside";

export function normalizePinnedTree(book: NotebookRecord): DocumentTreeNode[] {
  const documents = documentsInNotebook(book).map(({ document }) => document);
  const eligible = new Set(documents.filter((doc) => doc.pinned && !doc.trashed).map((doc) => doc.id));
  const seen = new Set<string>();
  const visit = (nodes: readonly DocumentTreeNode[]): DocumentTreeNode[] => nodes.flatMap((node) => {
    const keep = eligible.has(node.id) && !seen.has(node.id);
    if (keep) seen.add(node.id);
    const children = visit(node.children ?? []);
    return keep ? [{ id: node.id, ...(children.length ? { children } : {}) }] : children;
  });
  const result = visit(book.pinnedTree ?? []);
  for (const doc of documents) {
    if (eligible.has(doc.id) && !seen.has(doc.id)) {
      result.push({ id: doc.id });
      seen.add(doc.id);
    }
  }
  return result;
}

export function findTreeNode(nodes: readonly DocumentTreeNode[], id: string): DocumentTreeNode | undefined {
  for (const node of nodes) {
    if (node.id === id) return node;
    const found = findTreeNode(node.children ?? [], id);
    if (found) return found;
  }
}

export function movePinnedTree(
  nodes: readonly DocumentTreeNode[], id: string, target: string | null, placement: TreePlacement,
): DocumentTreeNode[] {
  const source = findTreeNode(nodes, id);
  if (!source || (target !== null && (id === target || findTreeNode(source.children ?? [], target) || !findTreeNode(nodes, target)))) {
    return [...nodes];
  }
  const remove = (items: readonly DocumentTreeNode[]): DocumentTreeNode[] => items.filter((item) => item.id !== id).map((item) => ({ ...item, children: remove(item.children ?? []) }));
  const insert = (items: readonly DocumentTreeNode[]): DocumentTreeNode[] => items.flatMap((item) => {
    if (item.id === target) {
      if (placement === "inside") return [{ ...item, children: [...(item.children ?? []), source] }];
      return placement === "before" ? [source, item] : [item, source];
    }
    return [{ ...item, children: insert(item.children ?? []) }];
  });
  const remaining = remove(nodes);
  return target === null ? [...remaining, source] : insert(remaining);
}
