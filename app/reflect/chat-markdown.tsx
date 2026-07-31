"use client";

import { useCallback } from "react";
import { MarkdownView } from "@meowdown/react";
import type { DocumentLocation } from "./types";

interface ChatMarkdownProps {
  text: string;
  documents: readonly DocumentLocation[];
  onOpenDocument: (documentId: string) => void;
  onOpenDocumentInNewWindow: (documentId: string) => void;
}

export function ChatMarkdown({
  text,
  documents,
  onOpenDocument,
  onOpenDocumentInNewWindow,
}: ChatMarkdownProps) {
  const handleWikiLink = useCallback(
    ({
      target,
      event,
    }: {
      target: string;
      event: MouseEvent | KeyboardEvent;
    }) => {
      const folded = target
        .split("#", 1)[0]
        .trim()
        .toLocaleLowerCase();
      const note = documents.find(
        ({ document }) =>
          document.title.toLocaleLowerCase() === folded ||
          document.id.toLocaleLowerCase() === folded ||
          document.aliases?.some(
            (alias) => alias.toLocaleLowerCase() === folded,
          ),
      );
      if (note) {
        if (event.metaKey || event.ctrlKey) {
          onOpenDocumentInNewWindow(note.document.id);
        } else {
          onOpenDocument(note.document.id);
        }
      }
    },
    [documents, onOpenDocument, onOpenDocumentInNewWindow],
  );
  const handleLink = useCallback(({ href }: { href: string }) => {
    if (/^https?:\/\//i.test(href)) {
      window.open(href, "_blank", "noopener,noreferrer");
    }
  }, []);

  return (
    <MarkdownView
      markdown={text}
      markMode="hide"
      interactive
      className="chat-markdown-view"
      onWikilinkClick={handleWikiLink}
      onLinkClick={handleLink}
    />
  );
}
