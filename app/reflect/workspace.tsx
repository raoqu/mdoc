"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type MouseEvent,
} from "react";
import {
  Archive,
  Check,
  ChevronLeft,
  ChevronRight,
  Eye,
  FilePlus2,
  Menu,
  MoreHorizontal,
  PanelRightClose,
  PanelRightOpen,
  Pin,
  PinOff,
  Shield,
  Trash2,
} from "lucide-react";
import dynamic from "next/dynamic";
import { AiSettings } from "./ai-settings";
import { AudioMemoPanel } from "./audio-memo";
import type { AiProviderConfig } from "./ai";
import { ChatScreen } from "./chat-screen";
import { CaptureSettings } from "./capture-settings";
import { TemplateSettings } from "./template-settings";
import { SyncSettings, type SyncConfig } from "./sync-settings";
import type { StoredTemplate } from "./templates";
import { CommandPalette } from "./command-palette";
import {
  backlinksFor,
  excerpt,
  formatDailyTitle,
  localDateKey,
  renameWikiLinks,
  rescheduleTask,
  tagsIn,
  tasksIn,
  titleFromMarkdown,
  toggleTask,
} from "./markdown";
import {
  aliasesFromFrontmatter,
  joinFrontmatter,
  privateFromFrontmatter,
  splitFrontmatter,
  upsertFrontmatter,
} from "./frontmatter";
import type { ReflectEditorHandle } from "./reflect-editor";
import {
  DEFAULT_SETTINGS,
  documentsInNotebook,
  removeDocument,
  updateDocument,
  updateFolders,
  type DocumentLocation,
  type DocumentRecord,
  type FolderRecord,
  type NotebookRecord,
  type WorkspaceSettings,
  type WorkspaceView,
} from "./types";
import { WorkspaceSidebar } from "./workspace-sidebar";

const ReflectEditor = dynamic(
  () => import("./reflect-editor").then((module) => module.ReflectEditor),
  {
    ssr: false,
    loading: () => <div className="editor-loading">正在加载编辑器…</div>,
  },
);

const WELCOME_NOTEBOOK: NotebookRecord = {
  id: "welcome",
  title: "我的知识库",
  description: "本地 SQLite 笔记库",
  accent: "#6d5bd0",
  folders: [
    {
      id: "welcome-notes",
      title: "笔记",
      open: true,
      children: [],
      docs: [
        {
          id: "hello",
          title: "欢迎使用墨笺",
          content:
            "# 欢迎使用墨笺\n\n这是迁移自 Reflect 的关联式 Markdown 编辑体验。\n\n- 输入 `[[` 链接另一篇笔记\n- 输入 `#标签` 建立主题\n- 输入 `/` 打开插入菜单\n- 使用 `+ [ ]` 创建可汇总的任务\n\n## 开始\n\n按下 **⌘K** 搜索笔记或执行命令。",
          updatedAt: new Date().toISOString(),
          createdAt: new Date().toISOString(),
          pinned: true,
          revision: 0,
        },
      ],
    },
  ],
};

function ensureFolder(
  notebook: NotebookRecord,
  id: string,
  title: string,
): { notebook: NotebookRecord; folder: FolderRecord } {
  const existing = notebook.folders.find((folder) => folder.id === id);
  if (existing) {
    return { notebook, folder: existing };
  }
  const folder: FolderRecord = {
    id,
    title,
    open: true,
    docs: [],
    children: [],
  };
  return {
    notebook: { ...notebook, folders: [...notebook.folders, folder] },
    folder,
  };
}

function addDocumentToFolder(
  notebook: NotebookRecord,
  folderId: string,
  folderTitle: string,
  document: DocumentRecord,
): NotebookRecord {
  const ensured = ensureFolder(notebook, folderId, folderTitle).notebook;
  return {
    ...ensured,
    folders: updateFolders(ensured.folders, (folder) =>
      folder.id === folderId
        ? { ...folder, docs: [...folder.docs, document] }
        : folder,
    ),
  };
}

function sameView(left: WorkspaceView, right: WorkspaceView): boolean {
  return JSON.stringify(left) === JSON.stringify(right);
}

function dateOffset(date: string, days: number): string {
  const value = new Date(`${date}T12:00:00`);
  value.setDate(value.getDate() + days);
  return localDateKey(value);
}

function loadSettings(): WorkspaceSettings {
  try {
    const raw = window.localStorage.getItem("mdocman.reflect.settings");
    if (!raw) {
      return DEFAULT_SETTINGS;
    }
    return { ...DEFAULT_SETTINGS, ...(JSON.parse(raw) as Partial<WorkspaceSettings>) };
  } catch {
    return DEFAULT_SETTINGS;
  }
}

function normalizeNotebookMetadata(
  notebook: NotebookRecord,
): NotebookRecord {
  return {
    ...notebook,
    folders: updateFolders(notebook.folders, (folder) => ({
      ...folder,
      docs: folder.docs.map((document) => {
        const frontmatterAliases = aliasesFromFrontmatter(document.content);
        return {
          ...document,
          revision: document.revision ?? 0,
          private:
            document.private || privateFromFrontmatter(document.content),
          aliases:
            document.aliases && document.aliases.length > 0
              ? document.aliases
              : frontmatterAliases,
        };
      }),
    })),
  };
}

export function ReflectWorkspace() {
  const [notebooks, setNotebooks] = useState<NotebookRecord[]>([WELCOME_NOTEBOOK]);
  const [notebookId, setNotebookId] = useState(WELCOME_NOTEBOOK.id);
  const [loaded, setLoaded] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [conflicts, setConflicts] = useState<Record<string, DocumentRecord>>({});
  const [notice, setNotice] = useState("");
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [paletteInitialQuery, setPaletteInitialQuery] = useState("");
  const [audioMemoOpen, setAudioMemoOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [contextOpen, setContextOpen] = useState(true);
  const [settings, setSettings] = useState<WorkspaceSettings>(DEFAULT_SETTINGS);
  const [aiProviders, setAiProviders] = useState<AiProviderConfig[]>([]);
  const [templates, setTemplates] = useState<StoredTemplate[]>([]);
  const [syncConfig, setSyncConfig] = useState<SyncConfig>({ status: "disconnected" });
  const [history, setHistory] = useState<WorkspaceView[]>([
    { kind: "daily", date: localDateKey() },
  ]);
  const [historyIndex, setHistoryIndex] = useState(0);
  const editorRef = useRef<ReflectEditorHandle>(null);
  const structuralDirtyRef = useRef(false);
  const dirtyDocumentIdsRef = useRef(new Set<string>());
  const settledTitlesRef = useRef(new Map<string, string>());
  const pendingTitlesRef = useRef(new Map<string, string>());
  const renameTimersRef = useRef(new Map<string, number>());
  const syncTimerRef = useRef(0);
  const assetDescribeTimerRef = useRef(0);
  const launchActionHandledRef = useRef(false);

  const view = history[historyIndex] ?? history[0];
  const notebook =
    notebooks.find((candidate) => candidate.id === notebookId) ??
    notebooks[0] ??
    WELCOME_NOTEBOOK;
  const documents = useMemo(
    () => (notebook ? documentsInNotebook(notebook) : []),
    [notebook],
  );

  const toast = useCallback((message: string) => {
    setNotice(message);
    window.setTimeout(() => setNotice(""), 2600);
  }, []);

  const refreshNotebooks = useCallback(async () => {
    const response = await fetch("/api/notebooks");
    if (!response.ok) throw new Error(await response.text());
    const records = (await response.json()) as NotebookRecord[];
    if (records.length > 0) setNotebooks(records.map(normalizeNotebookMetadata));
  }, []);

  useEffect(() => {
    queueMicrotask(() => setSettings(loadSettings()));
    fetch("/api/notebooks")
      .then(async (response) => {
        if (!response.ok) {
          throw new Error(await response.text());
        }
        return (await response.json()) as NotebookRecord[];
      })
      .then((records) => {
        if (records.length > 0) {
          setNotebooks(records.map(normalizeNotebookMetadata));
          setNotebookId(records[0].id);
        }
      })
      .catch(() => toast("后端暂不可用，当前显示本地示例数据"))
      .finally(() => setLoaded(true));
  }, [toast]);

  useEffect(() => {
    fetch("/api/ai/providers")
      .then(async (response) => {
        if (!response.ok) throw new Error(await response.text());
        return (await response.json()) as AiProviderConfig[];
      })
      .then(setAiProviders)
      .catch(() => setAiProviders([]));
  }, []);

  useEffect(() => {
    fetch(`/api/templates?notebookId=${encodeURIComponent(notebookId)}`)
      .then(async (response) => {
        if (!response.ok) throw new Error(await response.text());
        return (await response.json()) as StoredTemplate[];
      })
      .then(setTemplates)
      .catch(() => setTemplates([]));
  }, [notebookId]);

  useEffect(() => {
    fetch(`/api/sync?notebookId=${encodeURIComponent(notebookId)}`)
      .then((response) => response.json())
      .then((value) => setSyncConfig(value as SyncConfig))
      .catch(() => setSyncConfig({ status: "disconnected" }));
  }, [notebookId]);

  useEffect(() => {
    const reconcileExternalWrites = async () => {
      if (dirty || document.visibilityState === "hidden") return;
      try {
        await refreshNotebooks();
      } catch {
        // The local service may be stopped while the browser remains open.
      }
    };
    const onFocus = () => void reconcileExternalWrites();
    const timer = window.setInterval(() => void reconcileExternalWrites(), 10_000);
    window.addEventListener("focus", onFocus);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("focus", onFocus);
    };
  }, [dirty, refreshNotebooks]);

  const triggerSync = useCallback(async () => {
    if (!syncConfig.remoteUrl || syncConfig.status === "backing_up") return;
    setSyncConfig((current) => ({ ...current, status: "backing_up" }));
    try {
      const response = await fetch(
        `/api/sync/run?notebookId=${encodeURIComponent(notebookId)}`,
        { method: "POST" },
      );
      if (!response.ok) throw new Error(await response.text());
      setSyncConfig((await response.json()) as SyncConfig);
      await refreshNotebooks();
    } catch (error) {
      setSyncConfig((current) => ({
        ...current,
        status: navigator.onLine ? "failed" : "offline",
        lastError: error instanceof Error ? error.message : "同步失败",
      }));
    }
  }, [notebookId, refreshNotebooks, syncConfig.remoteUrl, syncConfig.status]);

  const runAssetDescriptions = useCallback(async (silent = false) => {
    if (aiProviders.length === 0) {
      if (!silent) toast("请先添加 AI 供应商");
      return;
    }
    const response = await fetch("/api/assets/describe", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({}),
    });
    if (!response.ok) {
      toast(await response.text());
      return;
    }
    const result = (await response.json()) as {
      described: number;
      skippedPrivacy: number;
      skippedUnreferenced: number;
    };
    if (!silent || result.described > 0) {
      toast(
        `已描述 ${result.described} 个资源；隐私跳过 ${result.skippedPrivacy}，未引用跳过 ${result.skippedUnreferenced}`,
      );
    }
  }, [aiProviders.length, toast]);

  useEffect(() => {
    const retry = () => void triggerSync();
    if (syncConfig.autoSync && syncConfig.remoteUrl) {
      window.addEventListener("online", retry);
      window.addEventListener("focus", retry);
    }
    return () => {
      window.removeEventListener("online", retry);
      window.removeEventListener("focus", retry);
    };
  }, [syncConfig.autoSync, syncConfig.remoteUrl, triggerSync]);

  const persist = useCallback(async () => {
    if (!loaded || !dirty || saving) {
      return;
    }
    setSaving(true);
    try {
      if (structuralDirtyRef.current) {
        structuralDirtyRef.current = false;
        dirtyDocumentIdsRef.current.clear();
        const response = await fetch("/api/notebooks", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(notebooks),
        });
        if (!response.ok) {
          structuralDirtyRef.current = true;
          throw new Error(await response.text());
        }
        const persisted = (await response.json()) as NotebookRecord[];
        const revisions = new Map(
          persisted.flatMap((record) =>
            documentsInNotebook(record).map(
              ({ document }) => [document.id, document.revision] as const,
            ),
          ),
        );
        setNotebooks((current) =>
          current.map((record) => ({
            ...record,
            folders: updateFolders(record.folders, (folder) => ({
              ...folder,
              docs: folder.docs.map((document) => ({
                ...document,
                revision: revisions.get(document.id) ?? document.revision,
              })),
            })),
          })),
        );
      } else {
        const dirtyIds = Array.from(dirtyDocumentIdsRef.current).filter(
          (documentId) => conflicts[documentId] === undefined,
        );
        for (const documentId of dirtyIds) {
          const outgoing = notebooks
            .flatMap((record) => documentsInNotebook(record))
            .find(({ document }) => document.id === documentId)?.document;
          if (!outgoing) {
            dirtyDocumentIdsRef.current.delete(documentId);
            continue;
          }
          dirtyDocumentIdsRef.current.delete(documentId);
          const response = await fetch(
            `/api/documents/${encodeURIComponent(documentId)}`,
            {
              method: "PUT",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify(outgoing),
            },
          );
          if (response.status === 409) {
            const serverDocument = (await response.json()) as DocumentRecord;
            setConflicts((current) => ({
              ...current,
              [documentId]: serverDocument,
            }));
            continue;
          }
          if (!response.ok) {
            dirtyDocumentIdsRef.current.add(documentId);
            throw new Error(await response.text());
          }
          const persisted = (await response.json()) as DocumentRecord;
          setNotebooks((current) =>
            updateDocument(current, documentId, (latest) => {
              const unchangedSinceDispatch =
                latest.content === outgoing.content &&
                latest.title === outgoing.title &&
                latest.pinned === outgoing.pinned &&
                latest.trashed === outgoing.trashed &&
                latest.private === outgoing.private &&
                JSON.stringify(latest.aliases ?? []) ===
                  JSON.stringify(outgoing.aliases ?? []);
              if (unchangedSinceDispatch) {
                return persisted;
              }
              dirtyDocumentIdsRef.current.add(documentId);
              return { ...latest, revision: persisted.revision };
            }),
          );
        }
      }
      setDirty(
        structuralDirtyRef.current || dirtyDocumentIdsRef.current.size > 0,
      );
      if (syncConfig.autoSync && syncConfig.remoteUrl) {
        window.clearTimeout(syncTimerRef.current);
        syncTimerRef.current = window.setTimeout(() => void triggerSync(), 30_000);
      }
      if (settings.describeAssets && aiProviders.length > 0) {
        window.clearTimeout(assetDescribeTimerRef.current);
        assetDescribeTimerRef.current = window.setTimeout(
          () => void runAssetDescriptions(true),
          2_000,
        );
      }
    } catch {
      toast("保存失败，修改仍保留在当前页面");
    } finally {
      setSaving(false);
    }
  }, [aiProviders.length, conflicts, dirty, loaded, notebooks, runAssetDescriptions, saving, settings.describeAssets, syncConfig.autoSync, syncConfig.remoteUrl, toast, triggerSync]);

  useEffect(() => {
    if (!dirty || !loaded) {
      return;
    }
    const timer = window.setTimeout(() => void persist(), 800);
    return () => window.clearTimeout(timer);
  }, [dirty, loaded, notebooks, persist]);

  useEffect(() => {
    const flush = () => {
      if (dirty) {
        void persist();
      }
    };
    window.addEventListener("pagehide", flush);
    return () => window.removeEventListener("pagehide", flush);
  }, [dirty, persist]);

  useEffect(() => {
    document.documentElement.dataset.theme = settings.theme;
    document.documentElement.dataset.editorWidth = settings.editorWidth;
    document.documentElement.dataset.editorTextSize = settings.textSize;
    window.localStorage.setItem(
      "mdocman.reflect.settings",
      JSON.stringify(settings),
    );
  }, [settings]);

  const mutateNotebooks = useCallback(
    (
      update: (records: NotebookRecord[]) => NotebookRecord[],
      options?: { documentIds?: readonly string[]; structural?: boolean },
    ) => {
      setNotebooks((records) => update(records));
      if (options?.structural === false) {
        for (const documentId of options.documentIds ?? []) {
          dirtyDocumentIdsRef.current.add(documentId);
        }
      } else {
        structuralDirtyRef.current = true;
      }
      setDirty(true);
    },
    [],
  );

  useEffect(() => {
    for (const { document } of documents) {
      const settled = settledTitlesRef.current.get(document.id);
      if (settled === undefined) {
        settledTitlesRef.current.set(document.id, document.title);
        continue;
      }
      if (settled === document.title) {
        continue;
      }
      if (pendingTitlesRef.current.get(document.id) === document.title) {
        continue;
      }
      const previousTimer = renameTimersRef.current.get(document.id);
      if (previousTimer !== undefined) {
        window.clearTimeout(previousTimer);
      }
      pendingTitlesRef.current.set(document.id, document.title);
      const nextTitle = document.title;
      const timer = window.setTimeout(() => {
        const previousTitle = settledTitlesRef.current.get(document.id);
        if (
          previousTitle === undefined ||
          previousTitle === nextTitle ||
          nextTitle.trim() === ""
        ) {
          return;
        }
        mutateNotebooks((records) =>
          records.map((candidate) => ({
            ...candidate,
            folders: updateFolders(candidate.folders, (folder) => ({
              ...folder,
              docs: folder.docs.map((item) => {
                if (item.id === document.id) {
                  const aliases = Array.from(
                    new Set([...(item.aliases ?? []), previousTitle]),
                  );
                  return {
                    ...item,
                    aliases,
                    content: upsertFrontmatter(item.content, { aliases }),
                  };
                }
                const rewritten = renameWikiLinks(
                  item.content,
                  previousTitle,
                  nextTitle,
                );
                return rewritten === item.content
                  ? item
                  : {
                      ...item,
                      content: rewritten,
                      updatedAt: new Date().toISOString(),
                    };
              }),
            })),
          })),
        );
        settledTitlesRef.current.set(document.id, nextTitle);
        pendingTitlesRef.current.delete(document.id);
        renameTimersRef.current.delete(document.id);
        toast(`已将“${previousTitle}”的链接更新为“${nextTitle}”`);
      }, 800);
      renameTimersRef.current.set(document.id, timer);
    }
  }, [documents, mutateNotebooks, toast]);

  useEffect(
    () => () => {
      for (const timer of renameTimersRef.current.values()) {
        window.clearTimeout(timer);
      }
    },
    [],
  );

  const navigate = useCallback(
    (next: WorkspaceView) => {
      if (sameView(view, next)) {
        return;
      }
      setHistory((items) => [...items.slice(0, historyIndex + 1), next]);
      setHistoryIndex((index) => index + 1);
    },
    [historyIndex, view],
  );

  const newNote = useCallback(() => {
    const id = crypto.randomUUID();
    const now = new Date().toISOString();
    const document: DocumentRecord = {
      id,
      title: "未命名笔记",
      content: "# \n\n",
      createdAt: now,
      updatedAt: now,
      revision: 0,
    };
    mutateNotebooks((records) =>
      records.map((candidate) =>
        candidate.id === notebook.id
          ? addDocumentToFolder(candidate, `${candidate.id}-notes`, "笔记", document)
          : candidate,
      ),
    );
    navigate({ kind: "note", documentId: id });
    window.setTimeout(() => editorRef.current?.focus(), 0);
  }, [mutateNotebooks, navigate, notebook.id]);

  const newNotebook = useCallback(() => {
    const id = crypto.randomUUID();
    const next: NotebookRecord = {
      id,
      title: `新知识库 ${notebooks.length + 1}`,
      description: "本地 SQLite 笔记库",
      accent: "#6d5bd0",
      folders: [
        {
          id: `${id}-notes`,
          title: "笔记",
          open: true,
          docs: [],
          children: [],
        },
      ],
    };
    mutateNotebooks((records) => [...records, next]);
    setNotebookId(id);
    navigate({ kind: "all-notes" });
    toast("新知识库已创建");
  }, [mutateNotebooks, navigate, notebooks.length, toast]);

  const ensureDaily = useCallback(
    (date: string): string => {
      const id = `daily-${date}`;
      const existing = documents.find(({ document }) => document.id === id);
      if (existing) {
        return id;
      }
      const now = new Date().toISOString();
      const document: DocumentRecord = {
        id,
        title: formatDailyTitle(date),
        content: `# ${formatDailyTitle(date)}\n\n${settings.startWithBullet ? "- " : ""}`,
        createdAt: now,
        updatedAt: now,
        revision: 0,
      };
      mutateNotebooks((records) =>
        records.map((candidate) =>
          candidate.id === notebook.id
            ? addDocumentToFolder(
                candidate,
                `${candidate.id}-daily`,
                "每日笔记",
                document,
              )
            : candidate,
        ),
      );
      return id;
    },
    [documents, mutateNotebooks, notebook.id, settings.startWithBullet],
  );

  const appendDeepLinkLine = useCallback((rawText: string, task: boolean) => {
    const text = rawText.replace(/\s+/g, " ").trim().slice(0, 10_000);
    if (!text) return;
    const date = localDateKey();
    const id = `daily-${date}`;
    const line = task ? `+ [ ] ${text}` : `- ${text}`;
    const now = new Date().toISOString();
    mutateNotebooks((records) => records.map((candidate) => {
      if (candidate.id !== notebook.id) return candidate;
      const existing = documentsInNotebook(candidate).find(
        ({ document }) => document.id === id,
      );
      if (!existing) {
        return addDocumentToFolder(
          candidate,
          `${candidate.id}-daily`,
          "每日笔记",
          {
            id,
            title: formatDailyTitle(date),
            content: `# ${formatDailyTitle(date)}\n\n${line}\n`,
            createdAt: now,
            updatedAt: now,
            revision: 0,
          },
        );
      }
      const lines = existing.document.content.split("\n");
      if (lines.some((candidateLine) => candidateLine.trim() === line)) {
        return candidate;
      }
      return {
        ...candidate,
        folders: updateFolders(candidate.folders, (folder) => ({
          ...folder,
          docs: folder.docs.map((document) =>
            document.id === id
              ? {
                  ...document,
                  content: `${document.content.trimEnd()}\n${line}\n`,
                  updatedAt: now,
                }
              : document,
          ),
        })),
      };
    }));
    navigate({ kind: "daily", date });
    toast(task ? "任务已添加到今天" : "内容已添加到今天");
  }, [mutateNotebooks, navigate, notebook.id, toast]);

  useEffect(() => {
    if (loaded && view.kind === "daily") {
      queueMicrotask(() => ensureDaily(view.date));
    }
  }, [ensureDaily, loaded, view]);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      const modifier = event.metaKey || event.ctrlKey;
      if (modifier && event.key.toLocaleLowerCase() === "k") {
        event.preventDefault();
        setPaletteOpen(true);
      } else if (modifier && event.key.toLocaleLowerCase() === "n") {
        event.preventDefault();
        newNote();
      } else if (modifier && event.key.toLocaleLowerCase() === "d") {
        event.preventDefault();
        navigate({ kind: "daily", date: localDateKey() });
      } else if (modifier && event.key.toLocaleLowerCase() === "j") {
        event.preventDefault();
        if (event.shiftKey && editorRef.current?.getSelectedText()) {
          editorRef.current.openSelectionMenu();
        } else {
          navigate({ kind: "chat" });
        }
      } else if (modifier && event.key.toLocaleLowerCase() === "t") {
        event.preventDefault();
        navigate({ kind: "tasks" });
      } else if (
        modifier &&
        event.shiftKey &&
        event.key.toLocaleLowerCase() === "o" &&
        (view.kind === "note" || view.kind === "daily")
      ) {
        event.preventDefault();
        const documentId =
          view.kind === "note" ? view.documentId : `daily-${view.date}`;
        const url = new URL(window.location.origin);
        url.searchParams.set("view", "note");
        url.searchParams.set("target", documentId);
        window.open(url, "_blank", "noopener,noreferrer");
      } else if (modifier && event.key === "\\") {
        event.preventDefault();
        setSidebarCollapsed((value) => !value);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [navigate, newNote, view]);

  useEffect(() => {
    const media = window.matchMedia("(max-width: 720px)");
    if (media.matches) setSidebarCollapsed(true);
  }, []);

  useEffect(() => {
    if (!loaded || launchActionHandledRef.current) return;
    launchActionHandledRef.current = true;
    const params = new URLSearchParams(window.location.search);
    const requestedView = params.get("view");
    const requestedAction = params.get("action");
    if (requestedAction === "new") newNote();
    else if (requestedAction === "append" || requestedAction === "task") {
      const text = params.get("text") ?? "";
      if (text && window.confirm(`允许这个链接把以下内容写入今日笔记？\n\n${text.slice(0, 240)}`)) {
        appendDeepLinkLine(text, requestedAction === "task");
      }
    }
    else if (requestedView === "tasks") navigate({ kind: "tasks" });
    else if (requestedView === "search") {
      setPaletteInitialQuery(params.get("q") ?? "");
      setPaletteOpen(true);
    } else if (requestedView === "note") {
      const target = (params.get("target") ?? "").trim().toLocaleLowerCase();
      const match = documents.find(({ document }) =>
        document.id.toLocaleLowerCase() === target ||
        document.title.toLocaleLowerCase() === target ||
        (document.aliases ?? []).some((alias) => alias.toLocaleLowerCase() === target)
      );
      if (match) navigate({ kind: "note", documentId: match.document.id });
      else toast("深链指向的笔记不存在");
    } else if (requestedView === "daily") {
      const date = params.get("date") ?? localDateKey();
      navigate({ kind: "daily", date });
    }
    if (params.has("action") || params.has("view")) {
      window.history.replaceState(null, "", window.location.pathname);
    }
  }, [appendDeepLinkLine, documents, loaded, navigate, newNote, toast]);

  const copyDeepLink = useCallback(async (location: DocumentLocation) => {
    const url = new URL(window.location.origin);
    if (location.document.id.startsWith("daily-")) {
      url.searchParams.set("view", "daily");
      url.searchParams.set("date", location.document.id.slice(6));
    } else {
      url.searchParams.set("view", "note");
      url.searchParams.set("target", location.document.id);
    }
    try {
      await navigator.clipboard.writeText(url.toString());
      toast("已复制笔记链接");
    } catch {
      toast(url.toString());
    }
  }, [toast]);

  const selected =
    view.kind === "note"
      ? documents.find(({ document }) => document.id === view.documentId)
      : view.kind === "daily"
        ? documents.find(({ document }) => document.id === `daily-${view.date}`)
        : undefined;

  const changeDocument = useCallback(
    (documentId: string, markdown: string) => {
      mutateNotebooks(
        (records) =>
          updateDocument(records, documentId, (document) => ({
            ...document,
            content: markdown,
            title: titleFromMarkdown(markdown, document.title),
            updatedAt: new Date().toISOString(),
          })),
        { documentIds: [documentId], structural: false },
      );
    },
    [mutateNotebooks],
  );

  const patchDocument = useCallback(
    (documentId: string, patch: Partial<DocumentRecord>) => {
      mutateNotebooks(
        (records) =>
          updateDocument(records, documentId, (document) => ({
            ...document,
            ...patch,
            content:
              patch.private === undefined
                ? document.content
                : upsertFrontmatter(document.content, {
                    private: patch.private ? true : undefined,
                  }),
            updatedAt: new Date().toISOString(),
          })),
        { documentIds: [documentId], structural: false },
      );
    },
    [mutateNotebooks],
  );

  const toggleDocumentTask = useCallback(
    (documentId: string, line: number, expectedContent: string) => {
      try {
        mutateNotebooks(
          (records) =>
            updateDocument(records, documentId, (document) => ({
              ...document,
              content: toggleTask(document.content, line, expectedContent),
              updatedAt: new Date().toISOString(),
            })),
          { documentIds: [documentId], structural: false },
        );
      } catch (error) {
        toast(error instanceof Error ? error.message : "无法更新任务");
      }
    },
    [mutateNotebooks, toast],
  );

  const scheduleDocumentTask = useCallback(
    (documentId: string, line: number, expectedContent: string, date: string) => {
      try {
        mutateNotebooks(
          (records) =>
            updateDocument(records, documentId, (document) => ({
              ...document,
              content: rescheduleTask(
                document.content,
                line,
                expectedContent,
                date || null,
              ),
              updatedAt: new Date().toISOString(),
            })),
          { documentIds: [documentId], structural: false },
        );
      } catch (error) {
        toast(error instanceof Error ? error.message : "无法安排任务");
      }
    },
    [mutateNotebooks, toast],
  );

  const handleTogglePinned = useCallback(
    (event: MouseEvent<HTMLButtonElement>) => {
      const documentId = event.currentTarget.dataset.documentId;
      if (!documentId) {
        return;
      }
      const current = documents.find(
        ({ document }) => document.id === documentId,
      )?.document;
      if (current) {
        patchDocument(documentId, { pinned: !current.pinned });
      }
    },
    [documents, patchDocument],
  );

  const handlePrivateChange = useCallback(
    (event: ChangeEvent<HTMLInputElement>) => {
      const documentId = event.currentTarget.dataset.documentId;
      if (documentId) {
        patchDocument(documentId, { private: event.currentTarget.checked });
      }
    },
    [patchDocument],
  );

  const handleMoveToTrash = useCallback(
    (event: MouseEvent<HTMLButtonElement>) => {
      const documentId = event.currentTarget.dataset.documentId;
      if (!documentId) {
        return;
      }
      patchDocument(documentId, { trashed: true });
      navigate({ kind: "all-notes" });
      toast("笔记已移到废纸篓");
    },
    [navigate, patchDocument, toast],
  );

  const handleRestoreFromTrash = useCallback(
    (event: MouseEvent<HTMLButtonElement>) => {
      const documentId = event.currentTarget.dataset.documentId;
      if (documentId) {
        patchDocument(documentId, { trashed: false });
      }
    },
    [patchDocument],
  );

  const handleDeletePermanently = useCallback(
    (event: MouseEvent<HTMLButtonElement>) => {
      const documentId = event.currentTarget.dataset.documentId;
      if (!documentId) {
        return;
      }
      mutateNotebooks((records) => removeDocument(records, documentId));
      toast("笔记已永久删除");
    },
    [mutateNotebooks, toast],
  );

  const keepLocalConflict = useCallback((event: MouseEvent<HTMLButtonElement>) => {
    const documentId = event.currentTarget.dataset.documentId;
    if (!documentId) {
      return;
    }
    const serverDocument = conflicts[documentId];
    if (!serverDocument) {
      return;
    }
    setNotebooks((records) =>
      updateDocument(records, documentId, (document) => ({
        ...document,
        revision: serverDocument.revision,
      })),
    );
    setConflicts((current) => {
      const next = { ...current };
      delete next[documentId];
      return next;
    });
    dirtyDocumentIdsRef.current.add(documentId);
    setDirty(true);
  }, [conflicts]);

  const loadServerConflict = useCallback((event: MouseEvent<HTMLButtonElement>) => {
    const documentId = event.currentTarget.dataset.documentId;
    if (!documentId) {
      return;
    }
    const serverDocument = conflicts[documentId];
    if (!serverDocument) {
      return;
    }
    dirtyDocumentIdsRef.current.delete(documentId);
    setNotebooks((records) =>
      updateDocument(records, documentId, () => serverDocument),
    );
    setConflicts((current) => {
      const next = { ...current };
      delete next[documentId];
      return next;
    });
    setDirty(
      structuralDirtyRef.current || dirtyDocumentIdsRef.current.size > 0,
    );
  }, [conflicts]);

  const openMarkdownPreview = useCallback((location: DocumentLocation) => {
    const split = splitFrontmatter(location.document.content);
    const content = joinFrontmatter(
      split.header,
      editorRef.current?.getMarkdown() ?? split.body,
    );
    const form = window.document.createElement("form");
    form.method = "POST";
    form.action = `/api/preview/${encodeURIComponent(location.document.id)}`;
    form.target = "_blank";
    form.style.display = "none";
    for (const [name, value] of Object.entries({
      title: location.document.title,
      book: location.notebook.title,
      content,
    })) {
      const field = window.document.createElement("textarea");
      field.name = name;
      field.value = value;
      form.appendChild(field);
    }
    window.document.body.appendChild(form);
    form.submit();
    form.remove();
  }, []);

  const renderEditor = (location: DocumentLocation) => {
    const backlinks = backlinksFor(location, documents);
    const conflict = conflicts[location.document.id];
    return (
      <div className={`note-layout ${contextOpen ? "" : "context-closed"}`}>
        <section className="note-column">
          <header className="workspace-header">
            <div className="header-left">
              {sidebarCollapsed ? (
                <button
                  type="button"
                  onClick={() => setSidebarCollapsed(false)}
                  title="展开侧栏"
                  aria-label="展开侧栏"
                >
                  <Menu size={18} />
                </button>
              ) : null}
              {view.kind === "daily" ? (
                <>
                  <button
                    type="button"
                    onClick={() =>
                      navigate({
                        kind: "daily",
                        date: dateOffset(view.date, -1),
                      })
                    }
                    aria-label="前一天"
                  >
                    <ChevronLeft size={17} />
                  </button>
                  <button
                    type="button"
                    className="today-button"
                    onClick={() =>
                      navigate({ kind: "daily", date: localDateKey() })
                    }
                  >
                    今天
                  </button>
                  <button
                    type="button"
                    onClick={() =>
                      navigate({
                        kind: "daily",
                        date: dateOffset(view.date, 1),
                      })
                    }
                    aria-label="后一天"
                  >
                    <ChevronRight size={17} />
                  </button>
                </>
              ) : (
                <span className="header-path">
                  {location.folder.title} / {location.document.title}
                </span>
              )}
            </div>
            <div className="header-actions">
              <span className={`save-state ${dirty ? "dirty" : ""}`}>
                {saving ? "正在保存…" : dirty ? "等待保存" : "已保存"}
              </span>
              <button
                type="button"
                onClick={() => openMarkdownPreview(location)}
                title="在浏览器中预览 Markdown"
                aria-label="在浏览器中预览 Markdown"
              >
                <Eye size={18} />
              </button>
              <button
                type="button"
                data-document-id={location.document.id}
                className={location.document.pinned ? "active" : ""}
                onClick={handleTogglePinned}
                title={location.document.pinned ? "取消固定" : "固定笔记"}
                aria-label={location.document.pinned ? "取消固定" : "固定笔记"}
              >
                {location.document.pinned ? <PinOff size={17} /> : <Pin size={17} />}
              </button>
              <button
                type="button"
                onClick={() => setContextOpen((value) => !value)}
                title={contextOpen ? "关闭详情侧栏" : "打开详情侧栏"}
                aria-label={contextOpen ? "关闭详情侧栏" : "打开详情侧栏"}
              >
                {contextOpen ? (
                  <PanelRightClose size={18} />
                ) : (
                  <PanelRightOpen size={18} />
                )}
              </button>
              <button
                type="button"
                title="复制笔记链接"
                aria-label="复制笔记链接"
                onClick={() => void copyDeepLink(location)}
              >
                <MoreHorizontal size={18} />
              </button>
            </div>
          </header>
          {conflict ? (
            <div className="note-conflict-banner" role="alert">
              <div>
                <strong>这篇笔记已在其他窗口中修改</strong>
                <span>你的内容没有被覆盖。请选择要保留的版本。</span>
              </div>
              <button
                type="button"
                data-document-id={location.document.id}
                onClick={loadServerConflict}
              >
                加载对方版本
              </button>
              <button
                type="button"
                className="primary"
                data-document-id={location.document.id}
                onClick={keepLocalConflict}
              >
                保留我的版本
              </button>
            </div>
          ) : null}
          <div className="note-scroll">
            <ReflectEditor
              key={location.document.id}
              handleRef={editorRef}
              document={location.document}
              documents={documents}
              settings={settings}
              aiProviders={aiProviders}
              templates={templates}
              onChange={changeDocument}
              onNavigate={(documentId) =>
                navigate({ kind: "note", documentId })
              }
              onTag={(tag) => navigate({ kind: "tag", tag })}
              onNotice={toast}
              onConfigureAI={() => navigate({ kind: "settings" })}
            />
          </div>
        </section>
        {contextOpen ? (
          <aside className="context-sidebar">
            <section>
              <h3>笔记信息</h3>
              <dl>
                <div>
                  <dt>创建</dt>
                  <dd>
                    {new Date(
                      location.document.createdAt ||
                        location.document.updatedAt,
                    ).toLocaleDateString("zh-CN")}
                  </dd>
                </div>
                <div>
                  <dt>更新</dt>
                  <dd>
                    {new Date(location.document.updatedAt).toLocaleString(
                      "zh-CN",
                      { dateStyle: "short", timeStyle: "short" },
                    )}
                  </dd>
                </div>
                <div>
                  <dt>字数</dt>
                  <dd>{location.document.content.length}</dd>
                </div>
              </dl>
            </section>
            <section>
              <h3>反向链接 <span>{backlinks.length}</span></h3>
              {backlinks.map(({ document: backlink }) => (
                <button
                  type="button"
                  className="backlink-card"
                  key={backlink.id}
                  onClick={() =>
                    navigate({ kind: "note", documentId: backlink.id })
                  }
                >
                  <strong>{backlink.title}</strong>
                  <small>{excerpt(backlink.content, location.document.title, 120)}</small>
                </button>
              ))}
              {backlinks.length === 0 ? (
                <p className="empty-copy">
                  在其他笔记中输入 [[{location.document.title}]] 建立关联。
                </p>
              ) : null}
            </section>
            <section className="note-actions">
              <label>
                <input
                  type="checkbox"
                  data-document-id={location.document.id}
                  checked={location.document.private ?? false}
                  onChange={handlePrivateChange}
                />
                <Shield size={15} />
                私密笔记
              </label>
              <button
                type="button"
                data-document-id={location.document.id}
                onClick={handleMoveToTrash}
              >
                <Trash2 size={15} />
                移到废纸篓
              </button>
            </section>
          </aside>
        ) : null}
      </div>
    );
  };

  const renderAllNotes = (filteredDocuments = documents) => (
    <section className="collection-screen">
      <header className="collection-header">
        <div>
          <h1>{view.kind === "tag" ? `#${view.tag}` : "全部笔记"}</h1>
          <p>
            {filteredDocuments.filter(({ document }) => !document.trashed).length} 篇笔记
          </p>
        </div>
        <button type="button" className="primary-action" onClick={newNote}>
          <FilePlus2 size={16} /> 新建笔记
        </button>
      </header>
      <div className="notes-table">
        <div className="notes-row notes-heading">
          <span>标题</span><span>位置</span><span>更新时间</span>
        </div>
        {filteredDocuments
          .filter(({ document }) => !document.trashed)
          .sort(
            (left, right) =>
              new Date(right.document.updatedAt).getTime() -
              new Date(left.document.updatedAt).getTime(),
          )
          .map(({ document, folder }) => (
            <button
              type="button"
              className="notes-row"
              key={document.id}
              onClick={() =>
                navigate({ kind: "note", documentId: document.id })
              }
            >
              <span>
                {document.pinned ? <Pin size={13} /> : null}
                <strong>{document.title}</strong>
                <small>{excerpt(document.content, "", 100)}</small>
              </span>
              <span>{folder.title}</span>
              <span>{new Date(document.updatedAt).toLocaleDateString("zh-CN")}</span>
            </button>
          ))}
      </div>
    </section>
  );

  const renderTasks = () => {
    const tasks = tasksIn(documents);
    const today = localDateKey();
    const open = tasks.filter((task) => !task.checked);
    const completed = tasks.filter((task) => task.checked);
    const groups = [
      ["已逾期", open.filter((task) => task.dueDate && task.dueDate < today)],
      ["今天", open.filter((task) => task.dueDate === today)],
      ["即将到来", open.filter((task) => task.dueDate && task.dueDate > today)],
      ["未安排", open.filter((task) => !task.dueDate)],
    ] as const;
    const taskRows = (group: typeof tasks) =>
      group.map((task) => (
        <div className={`task-row ${task.checked ? "completed" : ""}`} key={task.id}>
          <button
            type="button"
            className="task-checkbox"
            onClick={() =>
              toggleDocumentTask(task.documentId, task.line, task.content)
            }
            aria-label={task.checked ? "标记为未完成" : "标记为已完成"}
          >
            {task.checked ? <Check size={13} /> : null}
          </button>
          <button
            type="button"
            className="task-content"
            onClick={() => navigate({ kind: "note", documentId: task.documentId })}
          >
            {task.breadcrumbs.length > 0 ? (
              <small className="task-breadcrumbs">{task.breadcrumbs.join(" → ")}</small>
            ) : null}
            <strong>{task.content.replace(/\s*\[\[\d{4}-\d{2}-\d{2}\]\]\s*/, " ").trim()}</strong>
            <small>{task.documentTitle}</small>
          </button>
          <input
            type="date"
            value={task.dueDate ?? ""}
            onChange={(event) =>
              scheduleDocumentTask(
                task.documentId,
                task.line,
                task.content,
                event.target.value,
              )
            }
            aria-label={`安排任务：${task.content}`}
          />
        </div>
      ));
    return (
      <section className="collection-screen tasks-screen">
        <header className="collection-header">
          <div>
            <h1>任务</h1>
            <p>{open.length} 项待完成</p>
          </div>
        </header>
        {groups.map(([label, group]) => (
          <section className="task-group" key={label}>
            <h2>{label}<span>{group.length}</span></h2>
            {taskRows(group)}
            {group.length === 0 ? (
              <p className="empty-copy">暂无任务</p>
            ) : null}
          </section>
        ))}
        <details className="task-group completed-tasks">
          <summary>已完成 <span>{completed.length}</span></summary>
          {taskRows(completed)}
        </details>
      </section>
    );
  };

  const renderTrash = () => {
    const trashed = documents.filter(({ document }) => document.trashed);
    return (
      <section className="collection-screen">
        <header className="collection-header">
          <div>
            <h1>废纸篓</h1>
            <p>{trashed.length} 篇已删除笔记</p>
          </div>
        </header>
        <div className="trash-list">
          {trashed.map(({ document, folder }) => (
            <div className="trash-row" key={document.id}>
              <div>
                <strong>{document.title}</strong>
                <small>{folder.title} · {excerpt(document.content, "", 100)}</small>
              </div>
              <button
                type="button"
                data-document-id={document.id}
                onClick={handleRestoreFromTrash}
              >
                恢复
              </button>
              <button
                type="button"
                className="danger"
                data-document-id={document.id}
                onClick={handleDeletePermanently}
              >
                永久删除
              </button>
            </div>
          ))}
          {trashed.length === 0 ? (
            <div className="trash-empty">
              <Trash2 size={24} />
              <p>废纸篓是空的</p>
            </div>
          ) : null}
        </div>
      </section>
    );
  };

  const renderSettings = () => (
    <section className="settings-screen">
      <header>
        <h1>设置</h1>
        <p>调整墨笺的编辑和阅读体验。</p>
      </header>
      <section>
        <h2>编辑器</h2>
        <SettingSelect
          label="Markdown 语法"
          description="控制未编辑语法标记的显示方式"
          value={settings.syntaxMode}
          options={[
            ["hide", "隐藏"],
            ["focus", "聚焦时显示"],
            ["show", "始终显示"],
          ]}
          onChange={(syntaxMode) =>
            setSettings((current) => ({ ...current, syntaxMode }))
          }
        />
        <SettingSelect
          label="文字大小"
          description="调整编辑器正文的阅读字号"
          value={settings.textSize}
          options={[
            ["small", "小"],
            ["medium", "中"],
            ["large", "大"],
          ]}
          onChange={(textSize) =>
            setSettings((current) => ({ ...current, textSize }))
          }
        />
        <SettingSelect
          label="内容宽度"
          description="选择专注阅读宽度或宽屏编辑"
          value={settings.editorWidth}
          options={[
            ["reading", "阅读宽度"],
            ["wide", "宽屏"],
          ]}
          onChange={(editorWidth) =>
            setSettings((current) => ({ ...current, editorWidth }))
          }
        />
        <SettingToggle
          label="拼写检查"
          description="使用浏览器拼写检查标记可能的错误"
          checked={settings.spellCheck}
          onChange={(spellCheck) =>
            setSettings((current) => ({ ...current, spellCheck }))
          }
        />
        <SettingToggle
          label="标题后自动开始列表"
          description="在标题末尾回车时开始项目符号"
          checked={settings.startWithBullet}
          onChange={(startWithBullet) =>
            setSettings((current) => ({ ...current, startWithBullet }))
          }
        />
      </section>
      <section>
        <h2>外观</h2>
        <SettingSelect
          label="主题"
          description="选择浅色、深色或跟随系统"
          value={settings.theme}
          options={[
            ["system", "跟随系统"],
            ["light", "浅色"],
            ["dark", "深色"],
          ]}
          onChange={(theme) =>
            setSettings((current) => ({ ...current, theme }))
          }
        />
      </section>
      <section>
        <h2>快捷键</h2>
        <div className="shortcut-grid">
          <span>搜索与命令 <kbd>⌘ K</kbd></span>
          <span>新建笔记 <kbd>⌘ N</kbd></span>
          <span>今日笔记 <kbd>⌘ D</kbd></span>
          <span>任务 <kbd>⌘ T</kbd></span>
          <span>AI Chat <kbd>⌘ J</kbd></span>
          <span>选区 AI <kbd>⌘ ⇧ J</kbd></span>
          <span>独立窗口 <kbd>⌘ ⇧ O</kbd></span>
          <span>显示/隐藏侧栏 <kbd>⌘ \\</kbd></span>
          <span>移动块 <kbd>⌥ ↑ / ↓</kbd></span>
          <span>打开光标下链接 <kbd>⌘ ↵</kbd></span>
        </div>
      </section>
      <AiSettings
        providers={aiProviders}
        onChange={setAiProviders}
        onNotice={toast}
      />
      <section>
        <h2>资源 AI 描述</h2>
        <SettingToggle
          label="自动描述新资源"
          description="仅处理被公开笔记引用、且未被任何私密笔记引用的图片、SVG 和 PDF"
          checked={settings.describeAssets}
          onChange={(describeAssets) =>
            setSettings((current) => ({ ...current, describeAssets }))
          }
        />
        <div className="setting-row">
          <div>
            <strong>描述现有资源</strong>
            <small>会调用你的默认 AI 供应商，可能产生费用；已有同哈希描述不会重复生成</small>
          </div>
          <button type="button" onClick={() => void runAssetDescriptions()}>开始</button>
        </div>
      </section>
      <TemplateSettings
        notebookId={notebook.id}
        templates={templates}
        onChange={setTemplates}
        onNotice={toast}
      />
      <CaptureSettings notebookId={notebook.id} onNotice={toast} />
      <SyncSettings
        notebookId={notebook.id}
        config={syncConfig}
        onChange={setSyncConfig}
        onSynced={() => void refreshNotebooks()}
        onNotice={toast}
      />
      <section>
        <h2>数据</h2>
        <div className="setting-row">
          <div>
            <strong>新建知识库</strong>
            <small>创建独立的笔记、模板、Chat 历史和导出范围</small>
          </div>
          <button type="button" onClick={newNotebook}>新建</button>
        </div>
        <div className="setting-row">
          <div>
            <strong>Markdown 导出</strong>
            <small>将当前知识库导出为保留目录结构的 ZIP</small>
          </div>
          <a href={`/api/export?notebookId=${notebook.id}`}>导出</a>
        </div>
      </section>
    </section>
  );

  let content;
  if (selected) {
    content = renderEditor(selected);
  } else if (view.kind === "all-notes") {
    content = renderAllNotes();
  } else if (view.kind === "tag") {
    content = renderAllNotes(
      documents.filter(({ document }) => tagsIn(document.content).includes(view.tag)),
    );
  } else if (view.kind === "tasks") {
    content = renderTasks();
  } else if (view.kind === "trash") {
    content = renderTrash();
  } else if (view.kind === "chat") {
    content = (
      <ChatScreen
        notebookId={notebook.id}
        initialConversationId={view.conversationId}
        providers={aiProviders}
        documents={documents}
        onOpenDocument={(documentId) =>
          navigate({ kind: "note", documentId })
        }
        onConfigureAI={() => navigate({ kind: "settings" })}
        onConversationChange={(conversationId) => {
          const next: WorkspaceView = { kind: "chat", conversationId };
          setHistory((items) =>
            items.map((item, index) => (index === historyIndex ? next : item)),
          );
        }}
        onNotice={toast}
      />
    );
  } else if (view.kind === "settings") {
    content = renderSettings();
  } else {
    content = (
      <div className="empty-state">
        <Archive size={28} />
        <h2>笔记不可用</h2>
        <button type="button" onClick={() => navigate({ kind: "all-notes" })}>
          返回全部笔记
        </button>
      </div>
    );
  }

  return (
    <main className={`reflect-app ${sidebarCollapsed ? "sidebar-collapsed" : ""}`}>
      {sidebarCollapsed ? (
        <button
          type="button"
          className="mobile-menu-toggle"
          onClick={() => setSidebarCollapsed(false)}
          aria-label="打开导航"
        >
          <Menu size={19} />
        </button>
      ) : (
        <button
          type="button"
          className="mobile-sidebar-backdrop"
          onClick={() => setSidebarCollapsed(true)}
          aria-label="关闭导航"
        />
      )}
      {!sidebarCollapsed ? (
        <WorkspaceSidebar
          notebook={notebook}
          notebooks={notebooks}
          documents={documents}
          view={view}
          onNavigate={navigate}
          onSearch={() => setPaletteOpen(true)}
          onNewNote={newNote}
          onPrevious={() => setHistoryIndex((index) => Math.max(0, index - 1))}
          onNext={() =>
            setHistoryIndex((index) => Math.min(history.length - 1, index + 1))
          }
          canGoPrevious={historyIndex > 0}
          canGoNext={historyIndex < history.length - 1}
          onCollapse={() => setSidebarCollapsed(true)}
          onNotebookChange={(nextNotebookId) => {
            setNotebookId(nextNotebookId);
            const next = notebooks.find((item) => item.id === nextNotebookId);
            const first = next ? documentsInNotebook(next).find(({ document }) => !document.trashed) : undefined;
            navigate(first ? { kind: "note", documentId: first.document.id } : { kind: "all-notes" });
          }}
          onAudioMemo={() => setAudioMemoOpen(true)}
        />
      ) : null}
      <div className="workspace-content">{content}</div>
      {paletteOpen ? (
        <CommandPalette
          notebookId={notebook.id}
          documents={documents}
          initialQuery={paletteInitialQuery}
          onClose={() => {
            setPaletteOpen(false);
            setPaletteInitialQuery("");
          }}
          onOpenDocument={(documentId) =>
            navigate({ kind: "note", documentId })
          }
          onToday={() => navigate({ kind: "daily", date: localDateKey() })}
          onNewNote={newNote}
          onTasks={() => navigate({ kind: "tasks" })}
          onSettings={() => navigate({ kind: "settings" })}
        />
      ) : null}
      {notice ? <div className="reflect-toast">{notice}</div> : null}
      <AudioMemoPanel
        open={audioMemoOpen}
        notebookId={notebook.id}
        providers={aiProviders}
        onClose={() => setAudioMemoOpen(false)}
        onOpenDocument={(documentId) => {
          setAudioMemoOpen(false);
          navigate({ kind: "note", documentId });
        }}
        onSaved={() => void refreshNotebooks()}
        onNotice={toast}
      />
    </main>
  );
}

interface SettingToggleProps {
  label: string;
  description: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}

function SettingToggle({
  label,
  description,
  checked,
  onChange,
}: SettingToggleProps) {
  return (
    <label className="setting-row">
      <span>
        <strong>{label}</strong>
        <small>{description}</small>
      </span>
      <input
        type="checkbox"
        className="switch"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
      />
    </label>
  );
}

interface SettingSelectProps<Value extends string> {
  label: string;
  description: string;
  value: Value;
  options: readonly (readonly [Value, string])[];
  onChange: (value: Value) => void;
}

function SettingSelect<Value extends string>({
  label,
  description,
  value,
  options,
  onChange,
}: SettingSelectProps<Value>) {
  return (
    <label className="setting-row">
      <span>
        <strong>{label}</strong>
        <small>{description}</small>
      </span>
      <select
        value={value}
        onChange={(event) => onChange(event.target.value as Value)}
      >
        {options.map(([optionValue, optionLabel]) => (
          <option key={optionValue} value={optionValue}>
            {optionLabel}
          </option>
        ))}
      </select>
    </label>
  );
}
