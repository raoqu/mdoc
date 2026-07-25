"use client";

import {
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  FilePlus2,
  Files,
  ListChecks,
  MessageCircle,
  Mic,
  PanelLeftClose,
  Search,
  Settings,
  Tag,
  Trash2,
} from "lucide-react";
import { allTags, localDateKey } from "./markdown";
import type {
  DocumentLocation,
  NotebookRecord,
  WorkspaceView,
} from "./types";

interface WorkspaceSidebarProps {
  notebook: NotebookRecord;
  notebooks: readonly NotebookRecord[];
  documents: readonly DocumentLocation[];
  view: WorkspaceView;
  onNavigate: (view: WorkspaceView) => void;
  onSearch: () => void;
  onNewNote: () => void;
  onPrevious: () => void;
  onNext: () => void;
  canGoPrevious: boolean;
  canGoNext: boolean;
  onCollapse: () => void;
  onNotebookChange: (notebookId: string) => void;
  onAudioMemo: () => void;
}

function isView(view: WorkspaceView, kind: WorkspaceView["kind"]): boolean {
  return view.kind === kind;
}

export function WorkspaceSidebar({
  notebook,
  notebooks,
  documents,
  view,
  onNavigate,
  onSearch,
  onNewNote,
  onPrevious,
  onNext,
  canGoPrevious,
  canGoNext,
  onCollapse,
  onNotebookChange,
  onAudioMemo,
}: WorkspaceSidebarProps) {
  const pinned = documents.filter(({ document }) => document.pinned && !document.trashed);
  const tags = allTags(documents).slice(0, 8);

  return (
    <aside className="reflect-sidebar">
      <div className="sidebar-topline">
        <button
          type="button"
          onClick={onCollapse}
          title="收起侧栏"
          aria-label="收起侧栏"
        >
          <PanelLeftClose size={17} />
        </button>
        <span />
        <button
          type="button"
          onClick={onPrevious}
          disabled={!canGoPrevious}
          title="后退"
          aria-label="后退"
        >
          <ChevronLeft size={17} />
        </button>
        <button
          type="button"
          onClick={onNext}
          disabled={!canGoNext}
          title="前进"
          aria-label="前进"
        >
          <ChevronRight size={17} />
        </button>
      </div>

      <button type="button" className="sidebar-search" onClick={onSearch}>
        <Search size={16} />
        <span>搜索</span>
        <kbd>⌘ K</kbd>
      </button>

      <nav className="sidebar-primary" aria-label="主要导航">
        <button
          type="button"
          className={isView(view, "daily") ? "active" : ""}
          onClick={() => onNavigate({ kind: "daily", date: localDateKey() })}
        >
          <CalendarDays size={18} />
          <span>每日笔记</span>
          <kbd>⌘ D</kbd>
        </button>
        <button type="button" onClick={onNewNote}>
          <FilePlus2 size={18} />
          <span>新建笔记</span>
          <kbd>⌘ N</kbd>
        </button>
        <button
          type="button"
          className={isView(view, "all-notes") ? "active" : ""}
          onClick={() => onNavigate({ kind: "all-notes" })}
        >
          <Files size={18} />
          <span>全部笔记</span>
        </button>
        <button
          type="button"
          className={isView(view, "tasks") ? "active" : ""}
          onClick={() => onNavigate({ kind: "tasks" })}
        >
          <ListChecks size={18} />
          <span>任务</span>
          <kbd>⌘ T</kbd>
        </button>
        <button
          type="button"
          className={isView(view, "chat") ? "active" : ""}
          onClick={() => onNavigate({ kind: "chat" })}
        >
          <MessageCircle size={18} />
          <span>AI Chat</span>
          <kbd>⌘ J</kbd>
        </button>
        <button type="button" onClick={onAudioMemo}>
          <Mic size={18} />
          <span>音频备忘</span>
        </button>
        <button
          type="button"
          className={isView(view, "trash") ? "active" : ""}
          onClick={() => onNavigate({ kind: "trash" })}
        >
          <Trash2 size={18} />
          <span>废纸篓</span>
        </button>
      </nav>

      {pinned.length > 0 ? (
        <section className="sidebar-section">
          <h3>已固定</h3>
          {pinned.map(({ document }) => (
            <button
              type="button"
              key={document.id}
              className={
                view.kind === "note" && view.documentId === document.id
                  ? "active"
                  : ""
              }
              onClick={() =>
                onNavigate({ kind: "note", documentId: document.id })
              }
            >
              <span className="note-dot" />
              <span>{document.title}</span>
            </button>
          ))}
        </section>
      ) : null}

      {tags.length > 0 ? (
        <section className="sidebar-section sidebar-tags">
          <h3>标签</h3>
          {tags.map((tag) => (
            <button
              type="button"
              key={tag}
              className={view.kind === "tag" && view.tag === tag ? "active" : ""}
              onClick={() => onNavigate({ kind: "tag", tag })}
            >
              <Tag size={14} />
              <span>{tag}</span>
            </button>
          ))}
        </section>
      ) : null}

      <div className="sidebar-footer">
        {notebooks.length > 1 ? (
          <select
            className="notebook-switcher"
            value={notebook.id}
            onChange={(event) => onNotebookChange(event.target.value)}
            aria-label="切换知识库"
          >
            {notebooks.map((candidate) => (
              <option value={candidate.id} key={candidate.id}>
                {candidate.title}
              </option>
            ))}
          </select>
        ) : null}
        <button
          type="button"
          className={isView(view, "settings") ? "active" : ""}
          onClick={() => onNavigate({ kind: "settings" })}
        >
          <span
            className="graph-swatch"
            style={{ backgroundColor: notebook.accent }}
          />
          <span>
            <strong>{notebook.title}</strong>
            <small>{documents.filter(({ document }) => !document.trashed).length} 篇笔记</small>
          </span>
          <Settings size={16} />
        </button>
      </div>
    </aside>
  );
}
