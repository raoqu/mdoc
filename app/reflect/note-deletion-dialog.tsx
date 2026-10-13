"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { noteDeletionConfirmation } from "./note-deletion";
import type { DocumentRecord } from "./types";

export function useNoteDeletionConfirmation() {
  const [deletionMessage, setDeletionMessage] = useState("");
  const pendingAnswer = useRef<((confirmed: boolean) => void) | null>(null);
  const confirmDeletion = useCallback((documents: readonly Pick<DocumentRecord, "title" | "content">[]) => {
    const message = noteDeletionConfirmation(documents);
    if (!message) return Promise.resolve(true);
    if (pendingAnswer.current) return Promise.resolve(false);
    return new Promise<boolean>((resolve) => {
      pendingAnswer.current = resolve;
      setDeletionMessage(message);
    });
  }, []);
  const answerDeletion = useCallback((confirmed: boolean) => {
    const resolve = pendingAnswer.current;
    pendingAnswer.current = null;
    setDeletionMessage("");
    resolve?.(confirmed);
  }, []);
  useEffect(() => () => {
    pendingAnswer.current?.(false);
    pendingAnswer.current = null;
  }, []);
  return { deletionMessage, confirmDeletion, answerDeletion };
}

export function NoteDeletionDialog({ message, onAnswer }: { message: string; onAnswer: (confirmed: boolean) => void }) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog || !message) return;
    const previousFocus = document.activeElement;
    dialog.showModal();
    return () => {
      dialog.close();
      if (previousFocus instanceof HTMLElement && previousFocus.isConnected) previousFocus.focus();
    };
  }, [message]);
  if (!message) return null;
  return (
    <dialog ref={dialogRef} className="note-deletion-dialog" role="alertdialog"
      aria-labelledby="note-deletion-title" aria-describedby="note-deletion-message"
      onCancel={(event) => { event.preventDefault(); onAnswer(false); }}>
      <h2 id="note-deletion-title">确认删除笔记</h2>
      <p id="note-deletion-message">{message}</p>
      <footer>
        <button type="button" onClick={() => onAnswer(false)}>取消</button>
        <button type="button" className="danger" onClick={() => onAnswer(true)}>确认删除</button>
      </footer>
    </dialog>
  );
}
