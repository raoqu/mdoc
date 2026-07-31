"use client";

import { useEffect, useRef, useState } from "react";
import {
  CalendarDays,
  Check,
  ChevronLeft,
  ChevronRight,
  FilePlus2,
  Files,
  FolderOpen,
  ListChecks,
  LocateFixed,
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

export interface KnowledgeBaseCatalog {
  directory: string;
  active: string;
  knowledgeBases: {
    name: string;
    label: string;
    color: string;
    sizeBytes: number;
    modifiedAt: string;
  }[];
}

interface WorkspaceSidebarProps {
  notebook: NotebookRecord;
  notebooks: readonly NotebookRecord[];
  knowledgeBaseCatalog: KnowledgeBaseCatalog | null;
  knowledgeBaseSwitching: boolean;
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
  onKnowledgeBaseChange: (knowledgeBaseName: string) => void;
  onKnowledgeBaseColorChange: (color: string) => void;
  onKnowledgeBaseCreate: () => void;
  onKnowledgeBaseReveal: () => void;
  onAudioMemo: () => void;
}

const KNOWLEDGE_BASE_COLORS = [
  { label: "靛蓝", value: "#5b4cf0" },
  { label: "紫色", value: "#8b5cf6" },
  { label: "玫红", value: "#d946ef" },
  { label: "红色", value: "#ef4444" },
  { label: "橙色", value: "#f97316" },
  { label: "绿色", value: "#22c55e" },
  { label: "青色", value: "#14b8a6" },
  { label: "蓝色", value: "#3b82f6" },
] as const;

function isView(view: WorkspaceView, kind: WorkspaceView["kind"]): boolean {
  return view.kind === kind;
}

export function WorkspaceSidebar({
  notebook,
  notebooks,
  knowledgeBaseCatalog,
  knowledgeBaseSwitching,
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
  onKnowledgeBaseChange,
  onKnowledgeBaseColorChange,
  onKnowledgeBaseCreate,
  onKnowledgeBaseReveal,
  onAudioMemo,
}: WorkspaceSidebarProps) {
  const [knowledgeBaseMenuOpen, setKnowledgeBaseMenuOpen] = useState(false);
  const [colorMenuOpen, setColorMenuOpen] = useState(false);
  const knowledgeBaseMenuRef = useRef<HTMLDivElement>(null);
  const pinned = documents.filter(({ document }) => document.pinned && !document.trashed);
  const tags = allTags(documents).slice(0, 8);
  const activeKnowledgeBase = knowledgeBaseCatalog?.knowledgeBases.find(
    (knowledgeBase) => knowledgeBase.name === knowledgeBaseCatalog.active,
  );
  const activeKnowledgeBaseLabel = activeKnowledgeBase?.label ?? "我的知识库";
  const activeKnowledgeBaseColor = activeKnowledgeBase?.color ?? notebook.accent;

  useEffect(() => {
    if (!knowledgeBaseMenuOpen) return;
    const closeOnOutsideClick = (event: PointerEvent) => {
      if (!knowledgeBaseMenuRef.current?.contains(event.target as Node)) {
        setKnowledgeBaseMenuOpen(false);
        setColorMenuOpen(false);
      }
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setKnowledgeBaseMenuOpen(false);
        setColorMenuOpen(false);
      }
    };
    document.addEventListener("pointerdown", closeOnOutsideClick);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("pointerdown", closeOnOutsideClick);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [knowledgeBaseMenuOpen]);

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
        <span className="sidebar-topline-spacer" />
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
            aria-label="切换笔记空间"
          >
            {notebooks.map((candidate) => (
              <option value={candidate.id} key={candidate.id}>
                {candidate.title}
              </option>
            ))}
          </select>
        ) : null}
        <div className="knowledge-base-footer" ref={knowledgeBaseMenuRef}>
          <button
            type="button"
            className="knowledge-base-trigger"
            onClick={() => {
              setKnowledgeBaseMenuOpen((open) => !open);
              setColorMenuOpen(false);
            }}
            disabled={!knowledgeBaseCatalog || knowledgeBaseSwitching}
            title={
              knowledgeBaseCatalog
                ? `${activeKnowledgeBaseLabel} · ${knowledgeBaseCatalog.directory}`
                : "正在读取知识库"
            }
            aria-label={`切换知识库：${activeKnowledgeBaseLabel}`}
            aria-haspopup="menu"
            aria-expanded={knowledgeBaseMenuOpen}
          >
            <span
              className="graph-swatch"
              style={{ backgroundColor: activeKnowledgeBaseColor }}
            />
            <span>{activeKnowledgeBaseLabel}</span>
          </button>
          <button
            type="button"
            className={`sidebar-settings-button ${isView(view, "settings") ? "active" : ""}`}
            aria-label="打开用户设置"
            title="用户设置"
            onClick={() => {
              setKnowledgeBaseMenuOpen(false);
              setColorMenuOpen(false);
              onNavigate({ kind: "settings" });
            }}
          >
            <Settings size={17} />
          </button>

          {knowledgeBaseMenuOpen && knowledgeBaseCatalog ? (
            <div
              className="knowledge-base-menu"
              role="menu"
              aria-label="切换知识库"
            >
              <div className="knowledge-base-list">
                {knowledgeBaseCatalog.knowledgeBases.map((knowledgeBase) => {
                  const current =
                    knowledgeBase.name === knowledgeBaseCatalog.active;
                  return (
                    <button
                      type="button"
                      role="menuitemradio"
                      aria-checked={current}
                      key={knowledgeBase.name}
                      onClick={() => {
                        setKnowledgeBaseMenuOpen(false);
                        if (!current) onKnowledgeBaseChange(knowledgeBase.name);
                      }}
                    >
                      <span
                        className="knowledge-base-menu-swatch"
                        style={{ backgroundColor: knowledgeBase.color }}
                      />
                      <span>{knowledgeBase.label}</span>
                      {current ? <Check size={15} /> : null}
                    </button>
                  );
                })}
              </div>

              <div className="knowledge-base-menu-separator" />

              <div
                className="knowledge-base-color-control"
                onPointerEnter={() => setColorMenuOpen(true)}
                onPointerLeave={() => setColorMenuOpen(false)}
              >
                <button
                  type="button"
                  role="menuitem"
                  aria-haspopup="menu"
                  aria-expanded={colorMenuOpen}
                  onClick={() => setColorMenuOpen((open) => !open)}
                >
                  <span
                    className="knowledge-base-menu-swatch"
                    style={{ backgroundColor: activeKnowledgeBaseColor }}
                  />
                  <span>知识库颜色</span>
                  <ChevronRight size={16} />
                </button>
                {colorMenuOpen ? (
                  <div
                    className="knowledge-base-color-menu"
                    role="menu"
                    aria-label="知识库颜色"
                  >
                    {KNOWLEDGE_BASE_COLORS.map((color) => (
                      <button
                        type="button"
                        role="menuitemradio"
                        aria-checked={color.value === activeKnowledgeBaseColor}
                        key={color.value}
                        onClick={() => onKnowledgeBaseColorChange(color.value)}
                      >
                        <span
                          className="knowledge-base-menu-swatch"
                          style={{ backgroundColor: color.value }}
                        />
                        <span>{color.label}</span>
                        {color.value === activeKnowledgeBaseColor ? (
                          <Check size={15} />
                        ) : null}
                      </button>
                    ))}
                  </div>
                ) : null}
              </div>

              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setKnowledgeBaseMenuOpen(false);
                  onKnowledgeBaseReveal();
                }}
              >
                <LocateFixed size={16} />
                <span>在 Finder 中显示知识库</span>
              </button>
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setKnowledgeBaseMenuOpen(false);
                  onKnowledgeBaseCreate();
                }}
              >
                <FolderOpen size={16} />
                <span>新建知识库…</span>
              </button>
              <button
                type="button"
                role="menuitem"
                className={isView(view, "settings") ? "active" : ""}
                onClick={() => {
                  setKnowledgeBaseMenuOpen(false);
                  onNavigate({ kind: "settings" });
                }}
              >
                <Settings size={16} />
                <span>用户设置</span>
              </button>
            </div>
          ) : null}
        </div>
      </div>
    </aside>
  );
}
