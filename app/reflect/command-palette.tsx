"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import {
  CalendarDays,
  FilePlus2,
  FileText,
  ListChecks,
  Search,
  Settings,
} from "lucide-react";
import { excerpt } from "./markdown";
import type { DocumentLocation } from "./types";

interface PaletteAction {
  id: string;
  label: string;
  detail: string;
  icon: typeof Search;
  run: () => void;
}

interface SearchHit {
  id: string;
  title: string;
  snippet: string;
  updatedAt: string;
}

interface CommandPaletteProps {
  notebookId: string;
  documents: readonly DocumentLocation[];
  initialQuery?: string;
  onClose: () => void;
  onOpenDocument: (documentId: string) => void;
  onToday: () => void;
  onNewNote: () => void;
  onTasks: () => void;
  onSettings: () => void;
}

export function CommandPalette({
  notebookId,
  documents,
  initialQuery = "",
  onClose,
  onOpenDocument,
  onToday,
  onNewNote,
  onTasks,
  onSettings,
}: CommandPaletteProps) {
  const [query, setQuery] = useState(initialQuery);
  const [activeIndex, setActiveIndex] = useState(0);
  const [searchHits, setSearchHits] = useState<SearchHit[] | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const normalized = query.trim();
    if (normalized === "") {
      return;
    }
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      fetch(
        `/api/search?q=${encodeURIComponent(normalized)}&notebookId=${encodeURIComponent(notebookId)}`,
        { signal: controller.signal },
      )
        .then(async (response) => {
          if (!response.ok) {
            throw new Error(await response.text());
          }
          return (await response.json()) as SearchHit[];
        })
        .then(setSearchHits)
        .catch((cause: unknown) => {
          if (!(cause instanceof DOMException && cause.name === "AbortError")) {
            setSearchHits([]);
          }
        });
    }, 120);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [notebookId, query]);

  const actions: PaletteAction[] = useMemo(
    () => [
      {
        id: "today",
        label: "打开今日笔记",
        detail: "跳到今天的每日笔记",
        icon: CalendarDays,
        run: onToday,
      },
      {
        id: "new",
        label: "新建笔记",
        detail: "创建一篇普通笔记",
        icon: FilePlus2,
        run: onNewNote,
      },
      {
        id: "tasks",
        label: "查看任务",
        detail: "汇总所有 + [ ] 任务",
        icon: ListChecks,
        run: onTasks,
      },
      {
        id: "settings",
        label: "打开设置",
        detail: "编辑器、外观和数据设置",
        icon: Settings,
        run: onSettings,
      },
    ],
    [onNewNote, onSettings, onTasks, onToday],
  );

  const entries = useMemo(() => {
    const folded = query.trim().toLocaleLowerCase();
    const actionEntries = actions
      .filter(
        (action) =>
          folded === "" ||
          `${action.label} ${action.detail}`
            .toLocaleLowerCase()
            .includes(folded),
      )
      .map((action) => ({
        id: `action:${action.id}`,
        label: action.label,
        detail: action.detail,
        icon: action.icon,
        run: action.run,
      }));
    const localMatches = documents.filter(
      ({ document }) =>
        !document.trashed &&
        (folded === "" ||
          `${document.title} ${document.content}`
            .toLocaleLowerCase()
            .includes(folded)),
    );
    const rankedMatches =
      folded !== "" && searchHits !== null
        ? searchHits.flatMap((hit) => {
            const match = documents.find(
              ({ document }) => document.id === hit.id,
            );
            return match ? [{ match, hit }] : [];
          })
        : localMatches.map((match) => ({ match, hit: null }));
    const noteEntries = rankedMatches
      .slice(0, folded ? 12 : 6)
      .map(({ match: { document, folder }, hit }) => ({
        id: `note:${document.id}`,
        label: document.title,
        detail: `${folder.title} · ${
          hit
            ? hit.snippet.replace(/<\/?mark>/g, "")
            : excerpt(document.content, folded, 90)
        }`,
        icon: FileText,
        run: () => onOpenDocument(document.id),
      }));
    return [...actionEntries, ...noteEntries];
  }, [actions, documents, onOpenDocument, query, searchHits]);

  const choose = (index: number) => {
    const entry = entries[index];
    if (!entry) {
      return;
    }
    entry.run();
    onClose();
  };

  return (
    <div className="palette-backdrop" role="presentation" onMouseDown={onClose}>
      <div
        className="command-palette"
        role="dialog"
        aria-modal="true"
        aria-label="搜索和命令"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="palette-search">
          <Search size={18} />
          <input
            ref={inputRef}
            autoFocus
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setActiveIndex(0);
              setSearchHits(null);
            }}
            placeholder="搜索笔记或输入命令…"
            aria-label="搜索笔记或命令"
            onKeyDown={(event) => {
              if (event.key === "ArrowDown") {
                event.preventDefault();
                setActiveIndex((value) =>
                  Math.min(value + 1, Math.max(0, entries.length - 1)),
                );
              } else if (event.key === "ArrowUp") {
                event.preventDefault();
                setActiveIndex((value) => Math.max(0, value - 1));
              } else if (event.key === "Enter") {
                event.preventDefault();
                choose(activeIndex);
              } else if (event.key === "Escape") {
                onClose();
              }
            }}
          />
          <kbd>ESC</kbd>
        </div>
        <div className="palette-results" role="listbox">
          {entries.map((entry, index) => {
            const Icon = entry.icon;
            return (
              <button
                key={entry.id}
                type="button"
                className={index === activeIndex ? "active" : ""}
                role="option"
                aria-selected={index === activeIndex}
                onMouseEnter={() => setActiveIndex(index)}
                onClick={() => choose(index)}
              >
                <Icon size={17} strokeWidth={1.8} />
                <span>
                  <strong>{entry.label}</strong>
                  <small>{entry.detail}</small>
                </span>
                <kbd>↵</kbd>
              </button>
            );
          })}
          {entries.length === 0 ? (
            <div className="palette-empty">没有匹配的笔记或命令</div>
          ) : null}
        </div>
      </div>
    </div>
  );
}
