"use client";

import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { Check, Globe, Link, Link2, MoreHorizontal, Shield, Trash2 } from "lucide-react";

interface NoteHeaderActionsProps {
  published: boolean;
  privateNote: boolean;
  publishDisabled: boolean;
  deleteDisabled: boolean;
  onPublishChange: (published: boolean) => void;
  onCopyLink: () => void;
  onCopyBacklink: () => void;
  onPrivateChange: (privateNote: boolean) => void;
  onDelete: () => void;
}

export function NoteHeaderActions({ published, privateNote, publishDisabled, deleteDisabled, onPublishChange, onCopyLink, onCopyBacklink, onPrivateChange, onDelete }: NoteHeaderActionsProps) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const focusLastRef = useRef(false);
  const menuId = useId();
  const closeMenu = () => {
    setOpen(false);
    triggerRef.current?.focus();
  };

  useEffect(() => {
    if (!open) return;
    const items = menuRef.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)");
    items?.[focusLastRef.current ? items.length - 1 : 0]?.focus();
    const onOutsideClick = (event: PointerEvent) => {
      if (!containerRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        setOpen(false);
        triggerRef.current?.focus();
      }
    };
    document.addEventListener("pointerdown", onOutsideClick);
    document.addEventListener("keydown", onEscape);
    return () => {
      document.removeEventListener("pointerdown", onOutsideClick);
      document.removeEventListener("keydown", onEscape);
    };
  }, [open]);

  const handleMenuKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const items = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") ?? []);
    const index = items.indexOf(document.activeElement as HTMLButtonElement);
    if (event.key === "ArrowDown" || event.key === "ArrowUp" || event.key === "Home" || event.key === "End") {
      event.preventDefault();
      const next = event.key === "Home" ? 0 : event.key === "End" ? items.length - 1 : (index + (event.key === "ArrowDown" ? 1 : -1) + items.length) % items.length;
      items[next]?.focus();
    } else if (event.key === "Tab") {
      event.preventDefault();
      closeMenu();
    }
  };

  return (
    <div className="note-header-controls">
      <button type="button" className={`note-publication-toggle ${published ? "active" : ""}`}
        aria-label={published ? "取消发布标记" : "标记为发布"} aria-pressed={published}
        title={publishDisabled ? "私密笔记不能标记为发布" : published ? "取消发布标记" : "标记为发布"}
        disabled={publishDisabled} onClick={() => onPublishChange(!published)}><Globe size={18} /></button>
      <div className="note-more-control" ref={containerRef}>
        <button type="button" ref={triggerRef} className={open ? "active" : ""}
          aria-label="更多笔记操作" title="更多笔记操作" aria-haspopup="menu" aria-expanded={open} aria-controls={open ? menuId : undefined}
          onClick={() => { focusLastRef.current = false; setOpen((current) => !current); }}
          onKeyDown={(event) => {
            if (event.key === "ArrowDown" || event.key === "ArrowUp") {
              event.preventDefault();
              focusLastRef.current = event.key === "ArrowUp";
              setOpen(true);
            }
          }}><MoreHorizontal size={18} /></button>
        {open ? (
          <div id={menuId} ref={menuRef} className="note-more-menu" role="menu" aria-label="笔记操作" onKeyDown={handleMenuKeyDown}>
            <button type="button" role="menuitem" onClick={() => { closeMenu(); onCopyLink(); }}><Link size={15} />复制链接</button>
            <button type="button" role="menuitem" onClick={() => { closeMenu(); onCopyBacklink(); }}><Link2 size={15} />复制反向链接</button>
            <hr />
            <button type="button" role="menuitemcheckbox" aria-checked={privateNote}
              onClick={() => { closeMenu(); onPrivateChange(!privateNote); }}>
              <Shield size={15} />私密笔记{privateNote ? <Check size={15} className="note-menu-check" /> : null}
            </button>
            <hr />
            <button type="button" role="menuitem" className="danger" disabled={deleteDisabled}
              title={deleteDisabled ? "笔记尚未保存" : "删除（移到废纸篓）"}
              onClick={() => { closeMenu(); onDelete(); }}><Trash2 size={15} />删除</button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
