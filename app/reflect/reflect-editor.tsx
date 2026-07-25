"use client";

import {
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type Ref,
} from "react";
import type {
  EditorHandle,
  PendingReplacementResolveHandler,
  SelectionMenuContext,
  SelectionMenuItem,
  SelectionMenuSearchHandler,
} from "@meowdown/react";
import { MarkdownView, MeowdownEditor, WikilinkHoverCard } from "@meowdown/react";
import {
  checkRoundTrip,
  type AcceptPendingReplacementOptions,
  type ImageClickPayload,
  type StartPendingReplacementOptions,
} from "@meowdown/core";
import {
  Bold,
  Braces,
  CheckSquare,
  Heading2,
  ImagePlus,
  Italic,
  Link as LinkIcon,
  Link2,
  List,
  Paperclip,
  Quote,
  RotateCcw,
  Trash2,
} from "lucide-react";
import { NOTE_TEMPLATES, type StoredTemplate } from "./templates";
import { joinFrontmatter, splitFrontmatter } from "./frontmatter";
import {
  BUILT_IN_AI_PROMPTS,
  consumeEventStream,
  type AiPrompt,
  type AiProviderConfig,
} from "./ai";
import type {
  DocumentLocation,
  DocumentRecord,
  WorkspaceSettings,
} from "./types";
import { hasConflictMarkers, resolveConflictMarkers } from "./markdown";

export interface ReflectEditorHandle {
  focus(): void;
  getMarkdown(): string;
  insertMarkdown(markdown: string): void;
  getSelectedText(): string;
  openSelectionMenu(): void;
  startPendingReplacement(options: StartPendingReplacementOptions): boolean;
  appendPendingReplacementText(text: string): void;
  acceptPendingReplacement(options?: AcceptPendingReplacementOptions): void;
  discardPendingReplacement(): void;
}

interface ReflectEditorProps {
  document: DocumentRecord;
  documents: readonly DocumentLocation[];
  settings: WorkspaceSettings;
  aiProviders: readonly AiProviderConfig[];
  templates: readonly StoredTemplate[];
  onChange: (documentId: string, markdown: string) => void;
  onNavigate: (documentId: string) => void;
  onTag: (tag: string) => void;
  onNotice: (message: string) => void;
  onConfigureAI: () => void;
  handleRef?: Ref<ReflectEditorHandle>;
}

function resolveImageUrl(source: string): string | undefined {
  if (
    source.startsWith("/uploads/") ||
    source.startsWith("data:image/") ||
    /^https?:\/\//i.test(source)
  ) {
    return source;
  }
  return undefined;
}

function normalizeWikiTarget(value: string): string {
  return value.trim().replace(/\.md$/i, "").toLocaleLowerCase();
}

interface ImageSizeEditorState {
  documentId: string;
  sourceMarkdown: string;
  occurrence: number;
  rootWidth: number;
  rootHeight: number;
  left: number;
  top: number;
}

function nthOccurrence(source: string, search: string, occurrence: number): number {
  let from = 0;
  for (let index = 0; index <= occurrence; index++) {
    const found = source.indexOf(search, from);
    if (found < 0) return -1;
    if (index === occurrence) return found;
    from = found + search.length;
  }
  return -1;
}

function withoutImageSize(source: string): string {
  return source.replace(/<!--\s*\{[^}]*\}\s*-->\s*$/, "");
}

interface ImageMetadata {
  width?: number;
  height?: number;
  href?: string;
}

function imageMetadata(source: string): ImageMetadata {
  const match = source.match(/<!--\s*(\{[^}]*\})\s*-->\s*$/);
  if (!match) return {};
  try {
    const parsed = JSON.parse(match[1]) as ImageMetadata;
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

export function normalizeLinkedImages(markdown: string): string {
  return markdown.replace(
    /\[(!\[[^\]\n]*\]\([^\n]*?\)(?:<!--\s*(\{[^}\n]*\})\s*-->)?)\]\(([^)\n]+)\)/g,
    (whole, image: string, rawMetadata: string | undefined, href: string) => {
      if (!rawMetadata) return whole;
      try {
        const metadata = JSON.parse(rawMetadata) as ImageMetadata;
        metadata.href = href.replace(/\\([\\()])/g, "$1");
        return `${withoutImageSize(image)}<!-- ${JSON.stringify(metadata)} -->`;
      } catch {
        return whole;
      }
    },
  );
}

export function ReflectEditor({
  document,
  documents,
  settings,
  aiProviders,
  templates,
  onChange,
  onNavigate,
  onTag,
  onNotice,
  onConfigureAI,
  handleRef,
}: ReflectEditorProps) {
  const editorRef = useRef<EditorHandle>(null);
  const selectedImageRootRef = useRef<HTMLElement | null>(null);
  const [imageSizeEditor, setImageSizeEditor] = useState<ImageSizeEditorState | null>(null);
  const aiRunRef = useRef<{
    prompt: AiPrompt;
    context: SelectionMenuContext;
    controller: AbortController;
  } | null>(null);
  const onChangeRef = useRef(onChange);
  const split = splitFrontmatter(document.content);
  const normalizedBody = normalizeLinkedImages(split.body);
  const fidelity = checkRoundTrip(normalizedBody);
  const previewWikiLink = useCallback((target: string) => {
    const normalized = normalizeWikiTarget(target);
    const match = documents.find(({ document: candidate }) =>
      normalizeWikiTarget(candidate.title) === normalized ||
      normalizeWikiTarget(candidate.id) === normalized ||
      (candidate.aliases ?? []).some((alias) => normalizeWikiTarget(alias) === normalized)
    );
    if (!match || match.document.trashed) return null;
    const preview = splitFrontmatter(match.document.content).body;
    return (
      <article className="wikilink-preview-body">
        <strong>{match.document.title}</strong>
        <MarkdownView
          markdown={preview}
          markMode="hide"
          interactive={false}
          resolveImageUrl={(source) => source.startsWith("/uploads/") ? source : undefined}
        />
      </article>
    );
  }, [documents]);

  useLayoutEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);

  useEffect(() => {
    if (normalizedBody === split.body) return;
    queueMicrotask(() =>
      onChangeRef.current(document.id, joinFrontmatter(split.header, normalizedBody)),
    );
  }, [document.id, normalizedBody, split.body, split.header]);

  useImperativeHandle(
    handleRef,
    () => ({
      focus: () => editorRef.current?.focus(),
      getMarkdown: () => editorRef.current?.getMarkdown() ?? "",
      insertMarkdown: (markdown) => editorRef.current?.insertMarkdown(markdown),
      getSelectedText: () => editorRef.current?.getSelectedText() ?? "",
      openSelectionMenu: () => editorRef.current?.openSelectionMenu(),
      startPendingReplacement: (options) =>
        editorRef.current?.startPendingReplacement(options) ?? false,
      appendPendingReplacementText: (text) =>
        editorRef.current?.appendPendingReplacementText(text),
      acceptPendingReplacement: (options) =>
        editorRef.current?.acceptPendingReplacement(options),
      discardPendingReplacement: () =>
        editorRef.current?.discardPendingReplacement(),
    }),
    [],
  );

  useEffect(
    () => () => {
      aiRunRef.current?.controller.abort();
      aiRunRef.current = null;
    },
    [document.id],
  );

  useEffect(() => {
    if (!imageSizeEditor) return;
    const selectedRoot = selectedImageRootRef.current;
    if (!selectedRoot) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setImageSizeEditor(null);
    };
    const closeOnOutsideClick = (event: PointerEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      if (
        selectedRoot.contains(target) ||
        (target instanceof Element && target.closest(".image-context-toolbar"))
      ) return;
      setImageSizeEditor(null);
    };
    window.addEventListener("keydown", closeOnEscape);
    window.addEventListener("pointerdown", closeOnOutsideClick, true);
    return () => {
      delete selectedRoot.dataset.imageSelected;
      if (selectedImageRootRef.current === selectedRoot) {
        selectedImageRootRef.current = null;
      }
      window.removeEventListener("keydown", closeOnEscape);
      window.removeEventListener("pointerdown", closeOnOutsideClick, true);
    };
  }, [imageSizeEditor]);

  const streamAiRun = useCallback(
    async (prompt: AiPrompt, context: SelectionMenuContext) => {
      aiRunRef.current?.controller.abort();
      const run = { prompt, context, controller: new AbortController() };
      aiRunRef.current = run;
      try {
        const provider =
          aiProviders.find((candidate) => candidate.isDefault) ?? aiProviders[0];
        if (!provider) {
          editorRef.current?.discardPendingReplacement();
          onConfigureAI();
          onNotice("请先在设置中添加 AI 供应商");
          return;
        }
        const response = await fetch("/api/ai/transform", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          signal: run.controller.signal,
          body: JSON.stringify({
            documentId: document.id,
            providerId: provider.id,
            promptBody: prompt.body,
            selectedText: context.selectedText,
          }),
        });
        await consumeEventStream(response, (event) => {
          if (aiRunRef.current !== run) return;
          if (event.type === "text-delta" && event.text) {
            editorRef.current?.appendPendingReplacementText(event.text);
          } else if (event.type === "error") {
            throw new Error(event.message || "AI 处理失败");
          }
        });
      } catch (error) {
        if (run.controller.signal.aborted) return;
        editorRef.current?.discardPendingReplacement();
        onNotice(error instanceof Error ? error.message : "AI 处理失败");
      }
    },
    [aiProviders, document.id, onConfigureAI, onNotice],
  );

  const runAiPrompt = useCallback(
    (prompt: AiPrompt, context: SelectionMenuContext) => {
      if (
        editorRef.current?.startPendingReplacement({
          from: context.from,
          to: context.to,
          mode: prompt.mode,
        })
      ) {
        void streamAiRun(prompt, context);
      }
    },
    [streamAiRun],
  );

  const searchSelectionMenu = useMemo<
    SelectionMenuSearchHandler | undefined
  >(() => {
    if (document.private) return undefined;
    return (query) => {
      if (aiProviders.length === 0) {
        return [
          {
            id: "configure-ai",
            label: "在设置中添加 AI 供应商…",
            onSelect: onConfigureAI,
          },
        ];
      }
      const needle = query.trim().toLocaleLowerCase();
      const items: SelectionMenuItem[] = BUILT_IN_AI_PROMPTS.filter(
        (prompt) =>
          needle === "" || prompt.label.toLocaleLowerCase().includes(needle),
      ).map((prompt) => ({
        id: prompt.id,
        label: prompt.label,
        onSelect: (context: SelectionMenuContext) =>
          runAiPrompt(prompt, context),
      }));
      if (query.trim()) {
        const prompt: AiPrompt = {
          id: "ad-hoc",
          label: query.trim(),
          body: query.trim(),
          mode: "replace",
        };
        items.push({
          id: prompt.id,
          label: prompt.label,
          detail: "作为自定义提示运行",
          onSelect: (context) => runAiPrompt(prompt, context),
        });
      }
      return items;
    };
  }, [aiProviders.length, document.private, onConfigureAI, runAiPrompt]);

  const handlePendingReplacementResolve = useCallback<
    PendingReplacementResolveHandler
  >(() => {
    aiRunRef.current?.controller.abort();
    aiRunRef.current = null;
  }, []);

  const retryAiRun = useCallback(() => {
    const run = aiRunRef.current;
    if (!run) return;
    if (
      editorRef.current?.startPendingReplacement({
        from: run.context.from,
        to: run.context.to,
        mode: run.prompt.mode,
      })
    ) {
      void streamAiRun(run.prompt, run.context);
    }
  }, [streamAiRun]);

  const handleDocumentChange = useCallback(() => {
    onChangeRef.current(
      document.id,
      joinFrontmatter(split.header, editorRef.current?.getMarkdown() ?? ""),
    );
  }, [document.id, split.header]);

  const searchWikiLinks = useCallback(
    (query: string) => {
      const folded = query.trim().toLocaleLowerCase();
      return documents
        .filter(
          ({ document: candidate }) =>
            !candidate.trashed &&
            (folded === "" ||
              candidate.title.toLocaleLowerCase().includes(folded) ||
              candidate.aliases?.some((alias) =>
                alias.toLocaleLowerCase().includes(folded),
              )),
        )
        .slice(0, 10)
        .map(({ document: candidate, folder }) => ({
          target: candidate.title,
          label: candidate.title,
          detail: folder.title,
        }));
    },
    [documents],
  );

  const searchTags = useCallback(
    (query: string) => {
      const folded = query.toLocaleLowerCase();
      const tags = new Set<string>();
      for (const { document: candidate } of documents) {
        for (const match of candidate.content.matchAll(
          /(^|[\s(])#([\p{L}\p{N}_/-]+)/gu,
        )) {
          if (match[2].toLocaleLowerCase().includes(folded)) {
            tags.add(match[2]);
          }
        }
      }
      return Array.from(tags)
        .slice(0, 10)
        .map((tag) => ({ tag, label: `#${tag}` }));
    },
    [documents],
  );

  const searchSlashItems = useCallback(
    (query: string) => {
      const builtIns = NOTE_TEMPLATES.filter((template) =>
        `${template.label} ${template.detail}`
          .toLocaleLowerCase()
          .includes(query.toLocaleLowerCase()),
      ).map((template) => ({
        id: template.id,
        label: template.label,
        detail: template.detail,
        keywords: ["模板", "template"],
        onSelect: () => editorRef.current?.insertMarkdown(template.markdown),
      }));
      const custom = templates
        .filter((template) =>
          template.title.toLocaleLowerCase().includes(query.toLocaleLowerCase()),
        )
        .map((template) => ({
          id: `custom:${template.id}`,
          label: template.title,
          detail: "自定义模板",
          keywords: ["模板", "template"],
          onSelect: () => editorRef.current?.insertMarkdown(template.content),
        }));
      return [...custom, ...builtIns];
    },
    [templates],
  );

  const handleWikiLink = useCallback(
    ({ target }: { target: string }) => {
      const folded = target.trim().toLocaleLowerCase();
      const match = documents.find(
        ({ document: candidate }) =>
          candidate.title.toLocaleLowerCase() === folded ||
          candidate.aliases?.some(
            (alias) => alias.toLocaleLowerCase() === folded,
          ),
      );
      if (match) {
        onNavigate(match.document.id);
      } else {
        onNotice(`未找到“${target}”，可通过新建笔记创建它`);
      }
    },
    [documents, onNavigate, onNotice],
  );

  const handleFilePaste = useCallback(
    async (file: File): Promise<string | undefined> => {
      const form = new FormData();
      form.append("file", file);
      const response = await fetch("/api/upload", {
        method: "POST",
        body: form,
      });
      if (!response.ok) {
        onNotice("附件上传失败");
        return undefined;
      }
      const result = (await response.json()) as { url?: string };
      return result.url;
    },
    [onNotice],
  );

  const handleLinkClick = useCallback(
    ({ href }: { href: string }) => {
      if (/^https?:\/\//i.test(href)) {
        window.open(href, "_blank", "noopener,noreferrer");
      }
    },
    [],
  );

  const insertImage = useCallback(() => {
    const input = window.document.createElement("input");
    input.type = "file";
    input.accept = "image/*";
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) {
        return;
      }
      const url = await handleFilePaste(file);
      if (url) {
        editorRef.current?.insertMarkdown(`![${file.name}](${url})`);
      }
    };
    input.click();
  }, [handleFilePaste]);

  const insertAttachment = useCallback(() => {
    const input = window.document.createElement("input");
    input.type = "file";
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) return;
      const url = await handleFilePaste(file);
      if (url) {
        const markdown = file.type.startsWith("image/")
          ? `![${file.name}](${url})`
          : `[${file.name}](${url})`;
        editorRef.current?.insertMarkdown(markdown);
      }
    };
    input.click();
  }, [handleFilePaste]);

  const resolveFileLink = useCallback(
    ({ href }: { href: string }) => href.startsWith("/uploads/"),
    [],
  );

  const resolveFileInfo = useCallback(async (href: string) => {
    if (!href.startsWith("/uploads/")) return undefined;
    const response = await fetch(href, { method: "HEAD" });
    const size = Number(response.headers.get("content-length"));
    return response.ok && Number.isFinite(size) ? { size } : undefined;
  }, []);

  const openFile = useCallback(({ href }: { href: string }) => {
    if (href.startsWith("/uploads/")) {
      window.open(href, "_blank", "noopener,noreferrer");
    }
  }, []);

  const selectImage = useCallback(({ event }: ImageClickPayload) => {
    const target = event.target;
    if (!(target instanceof Element)) return;
    const root = target.closest<HTMLElement>("prosekit-resizable-root");
    if (!root) return;
    const imageView = root.closest<HTMLElement>(".md-image-view");
    const content = imageView?.querySelector<HTMLElement>(".md-image-view-content");
    const sourceMarkdown = content?.textContent ?? "";
    if (!imageView || !sourceMarkdown) return;
    const metadata = imageMetadata(sourceMarkdown);
    if (
      metadata.href &&
      event instanceof MouseEvent &&
      (event.metaKey || event.ctrlKey)
    ) {
      window.open(metadata.href, "_blank", "noopener,noreferrer");
      return;
    }
    const matchingViews = Array.from(
      imageView.closest(".ProseMirror")?.querySelectorAll<HTMLElement>(".md-image-view") ?? [],
    ).filter((candidate) =>
      candidate.querySelector<HTMLElement>(".md-image-view-content")?.textContent === sourceMarkdown,
    );
    const occurrence = Math.max(0, matchingViews.indexOf(imageView));
    const rect = root.getBoundingClientRect();
    const panelWidth = 126;
    const panelHeight = 42;
    if (selectedImageRootRef.current) {
      delete selectedImageRootRef.current.dataset.imageSelected;
    }
    selectedImageRootRef.current = root;
    root.dataset.imageSelected = "true";
    setImageSizeEditor({
      documentId: document.id,
      sourceMarkdown,
      occurrence,
      rootWidth: Math.max(1, Math.round(rect.width)),
      rootHeight: Math.max(1, Math.round(rect.height)),
      left: Math.max(8, Math.min(rect.left, window.innerWidth - panelWidth - 8)),
      top: rect.top - panelHeight - 8 >= 8
        ? rect.top - panelHeight - 8
        : Math.min(window.innerHeight - panelHeight - 8, rect.bottom + 8),
    });
  }, [document.id]);

  const replaceSelectedImage = useCallback((action: "reset" | "delete" | "link") => {
    if (!imageSizeEditor) return;
    const editor = editorRef.current;
    if (!editor) return;
    const markdown = editor.getMarkdown();
    const index = nthOccurrence(
      markdown,
      imageSizeEditor.sourceMarkdown,
      imageSizeEditor.occurrence,
    );
    if (index < 0) {
      setImageSizeEditor(null);
      onNotice("图片内容已变化，请重新选择");
      return;
    }
    const from = index;
    const to = index + imageSizeEditor.sourceMarkdown.length;
    let replacement = imageSizeEditor.sourceMarkdown;
    if (action === "reset") {
      const metadata = imageMetadata(imageSizeEditor.sourceMarkdown);
      if (metadata.href) {
        const image = selectedImageRootRef.current?.querySelector("img");
        const width = image?.naturalWidth || imageSizeEditor.rootWidth;
        const height = image?.naturalHeight || imageSizeEditor.rootHeight;
        replacement = `${withoutImageSize(imageSizeEditor.sourceMarkdown)}<!-- ${JSON.stringify({ width, height, href: metadata.href })} -->`;
      } else {
        replacement = withoutImageSize(imageSizeEditor.sourceMarkdown);
      }
    } else if (action === "delete") {
      replacement = "";
    } else {
      const metadata = imageMetadata(imageSizeEditor.sourceMarkdown);
      const existing = metadata.href ?? "";
      const href = window.prompt("输入图片链接地址", existing)?.trim();
      if (!href) return;
      const root = selectedImageRootRef.current;
      const rect = root?.getBoundingClientRect();
      const width = metadata.width || Math.max(1, Math.round(Number(root?.dataset.width) || rect?.width || imageSizeEditor.rootWidth));
      const height = metadata.height || Math.max(1, Math.round(Number(root?.dataset.height) || rect?.height || imageSizeEditor.rootHeight));
      replacement = `${withoutImageSize(imageSizeEditor.sourceMarkdown)}<!-- ${JSON.stringify({ ...metadata, width, height, href })} -->`;
    }
    const next = `${markdown.slice(0, from)}${replacement}${markdown.slice(to)}`;
    editor.setMarkdown(next);
    onChangeRef.current(document.id, joinFrontmatter(split.header, next));
    setImageSizeEditor(null);
    if (action === "reset") onNotice("已恢复图片默认大小");
    else if (action === "delete") onNotice("图片已删除");
    else onNotice("图片链接已更新");
  }, [document.id, imageSizeEditor, onNotice, split.header]);

  const toolbarItems = [
    { label: "二级标题", icon: Heading2, markdown: "## 标题" },
    { label: "粗体", icon: Bold, markdown: "**粗体**" },
    { label: "斜体", icon: Italic, markdown: "_斜体_" },
    { label: "无序列表", icon: List, markdown: "- 列表项" },
    { label: "任务", icon: CheckSquare, markdown: "+ [ ] 待办事项" },
    { label: "引用", icon: Quote, markdown: "> 引用" },
    { label: "Wiki 链接", icon: Link2, markdown: "[[笔记标题]]" },
    { label: "代码块", icon: Braces, markdown: "```text\n代码\n```" },
  ];

  return (
    <div className="reflect-editor-frame">
      <div className="reflect-formatting-toolbar" aria-label="编辑器格式工具栏">
        {toolbarItems.map(({ label, icon: Icon, markdown }) => (
          <button
            key={label}
            type="button"
            title={label}
            aria-label={label}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => editorRef.current?.insertMarkdown(markdown)}
          >
            <Icon size={16} strokeWidth={1.8} />
          </button>
        ))}
        <span />
        <button
          type="button"
          title="上传图片"
          aria-label="上传图片"
          onMouseDown={(event) => event.preventDefault()}
          onClick={insertImage}
        >
          <ImagePlus size={16} strokeWidth={1.8} />
        </button>
        <button
          type="button"
          title="上传附件"
          aria-label="上传附件"
          onMouseDown={(event) => event.preventDefault()}
          onClick={insertAttachment}
        >
          <Paperclip size={16} strokeWidth={1.8} />
        </button>
        <div className="toolbar-hint">
          输入 <kbd>/</kbd> 插入内容 · 点击或拖拽图片可调整尺寸
        </div>
      </div>

      {fidelity === "lossy" ? (
        <div className="protected-note">
          <div className="protected-note-banner">
            {hasConflictMarkers(document.content)
              ? "同步发现了双方修改。请选择保留这个设备、另一设备或两边内容；处理前笔记保持只读。"
              : "这篇笔记包含当前编辑器无法无损往返的 Markdown，因此以只读方式打开，避免保存时丢失内容。"}
            {hasConflictMarkers(document.content) ? (
              <div className="conflict-actions">
                <button type="button" onClick={() => onChange(document.id, resolveConflictMarkers(document.content, "ours"))}>保留这个设备</button>
                <button type="button" onClick={() => onChange(document.id, resolveConflictMarkers(document.content, "theirs"))}>保留另一设备</button>
                <button type="button" onClick={() => onChange(document.id, resolveConflictMarkers(document.content, "both"))}>两边都保留</button>
              </div>
            ) : null}
          </div>
          <pre>{document.content}</pre>
        </div>
      ) : (
        <MeowdownEditor
          key={document.id}
          handleRef={editorRef}
          initialMarkdown={normalizedBody}
          mode={settings.syntaxMode}
          spellCheck={settings.spellCheck}
          blockHandle
          bulletAfterHeading={settings.startWithBullet}
          editorClassName="reflect-editor reflect-note-surface"
          placeholder="开始写作…"
          onDocChange={handleDocumentChange}
          onWikilinkSearch={searchWikiLinks}
          onWikilinkClick={handleWikiLink}
          onImageClick={selectImage}
          onTagSearch={searchTags}
          onTagClick={({ tag }) => onTag(tag)}
          onSlashMenuSearch={searchSlashItems}
          onSelectionMenuSearch={searchSelectionMenu}
          pendingReplacementActions={
            <button type="button" onClick={retryAiRun}>
              重试
            </button>
          }
          onPendingReplacementResolve={handlePendingReplacementResolve}
          onFilePaste={handleFilePaste}
          resolveFileLink={resolveFileLink}
          resolveFileInfo={resolveFileInfo}
          onFileClick={openFile}
          resolveImageUrl={resolveImageUrl}
          onLinkClick={handleLinkClick}
        >
          <WikilinkHoverCard className="wikilink-preview-card">
            {({ target }) => previewWikiLink(target)}
          </WikilinkHoverCard>
        </MeowdownEditor>
      )}

      {imageSizeEditor?.documentId === document.id ? (
        <div
          className="image-context-toolbar"
          role="toolbar"
          aria-label="图片工具栏"
          style={{ left: imageSizeEditor.left, top: imageSizeEditor.top }}
        >
          <button type="button" title="恢复默认大小" aria-label="恢复默认大小" onClick={() => replaceSelectedImage("reset")}><RotateCcw size={16} /></button>
          <button type="button" title="添加或修改链接" aria-label="添加或修改链接" onClick={() => replaceSelectedImage("link")}><LinkIcon size={16} /></button>
          <button type="button" className="danger" title="删除图片" aria-label="删除图片" onClick={() => replaceSelectedImage("delete")}><Trash2 size={16} /></button>
        </div>
      ) : null}
    </div>
  );
}
