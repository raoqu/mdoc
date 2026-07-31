"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import Image from "next/image";
import dynamic from "next/dynamic";
import {
  CalendarDays,
  FileText,
  History,
  ImagePlus,
  LoaderCircle,
  MessageCircle,
  Paperclip,
  Plus,
  Search,
  Send,
  Square,
  Trash2,
} from "lucide-react";
import {
  chatModelOptions,
  consumeEventStream,
  resolveChatModel,
  type AiProviderConfig,
  type ChatModelSelection,
} from "./ai";
import type { DocumentLocation } from "./types";
import {
  imageFilesFrom,
  toChatAttachment,
  type ChatAttachment,
} from "./chat-attachments";
import { interleaveChatTools } from "./chat-transcript";

interface Conversation {
  id: string;
  notebookId: string;
  title: string;
  createdAt: string;
  updatedAt: string;
}

interface Message {
  id: string;
  conversationId: string;
  role: "user" | "assistant";
  content: string;
  createdAt: string;
  attachments?: ChatAttachment[];
  sources?: { id: string; title: string }[];
  tools?: ChatToolActivity[];
}

interface ChatToolActivity {
  toolCallId: string;
  tool: string;
  summary: string;
  status: "pending" | "complete" | "error";
  textOffset?: number;
  sources?: { id: string; title: string }[];
}

interface ChatScreenProps {
  notebookId: string;
  initialConversationId?: string;
  providers: readonly AiProviderConfig[];
  modelSelection: ChatModelSelection | null;
  chatSystemPrompt: string;
  semanticSearchEnabled: boolean;
  documents: readonly DocumentLocation[];
  onOpenDocument: (documentId: string) => void;
  onOpenDocumentInNewWindow: (documentId: string) => void;
  onConfigureAI: () => void;
  onModelSelectionChange: (selection: ChatModelSelection) => void;
  onConversationChange: (conversationId?: string) => void;
  onNotice: (message: string) => void;
}

const CHAT_IDLE_CUTOFF_MS = 6 * 60 * 60 * 1000;

const ChatMarkdown = dynamic(
  () => import("./chat-markdown").then((module) => module.ChatMarkdown),
  { ssr: false },
);

function mergeSources(
  current: readonly { id: string; title: string }[] = [],
  incoming: readonly { id: string; title: string }[] = [],
) {
  const merged = new Map(current.map((source) => [source.id, source]));
  for (const source of incoming) merged.set(source.id, source);
  return [...merged.values()];
}

function toolIcon(tool: string, pending: boolean) {
  if (pending) return <LoaderCircle className="chat-tool-spinner" size={13} />;
  if (tool === "search_notes") return <Search size={13} />;
  if (tool === "read_notes") return <FileText size={13} />;
  if (tool === "list_recent_notes") return <History size={13} />;
  if (tool === "list_daily_notes") return <CalendarDays size={13} />;
  return <Paperclip size={13} />;
}

function ToolActivity({
  activity,
  onOpenDocument,
  onOpenDocumentInNewWindow,
}: {
  activity: ChatToolActivity;
  onOpenDocument: (documentId: string) => void;
  onOpenDocumentInNewWindow: (documentId: string) => void;
}) {
  return (
    <div className={`chat-tool-activity ${activity.status}`}>
      {toolIcon(activity.tool, activity.status === "pending")}
      <span>{activity.summary}</span>
      {activity.sources?.map((source) => (
        <button
          type="button"
          key={source.id}
          onClick={(event) =>
            event.metaKey || event.ctrlKey
              ? onOpenDocumentInNewWindow(source.id)
              : onOpenDocument(source.id)
          }
        >
          {source.title}
        </button>
      ))}
    </div>
  );
}

function ChatMessageBody({
  message,
  streaming,
  documents,
  onOpenDocument,
  onOpenDocumentInNewWindow,
}: {
  message: Message;
  streaming: boolean;
  documents: readonly DocumentLocation[];
  onOpenDocument: (documentId: string) => void;
  onOpenDocumentInNewWindow: (documentId: string) => void;
}) {
  const renderText = (text: string, key: string) =>
    streaming ? (
      <span className="chat-streaming-text" key={key}>
        {text}
      </span>
    ) : (
      <ChatMarkdown
        key={key}
        text={text}
        documents={documents}
        onOpenDocument={onOpenDocument}
        onOpenDocumentInNewWindow={onOpenDocumentInNewWindow}
      />
    );
  const parts = interleaveChatTools(message.content, message.tools ?? []);

  if ((message.tools ?? []).length === 0) {
    return (
      <div className="chat-message-body">
        {renderText(
          message.content || (streaming ? "正在查找相关笔记…" : ""),
          "only-text",
        )}
      </div>
    );
  }

  return (
    <div className="chat-message-body chat-assistant-parts">
      {parts.map((part, index) =>
        part.kind === "text" ? (
          renderText(part.text, `text-${index}`)
        ) : (
          <ToolActivity
            key={`${part.activity.toolCallId}-${index}`}
            activity={part.activity}
            onOpenDocument={onOpenDocument}
            onOpenDocumentInNewWindow={onOpenDocumentInNewWindow}
          />
        ),
      )}
    </div>
  );
}

export function ChatScreen({
  notebookId,
  initialConversationId,
  providers,
  modelSelection,
  chatSystemPrompt,
  semanticSearchEnabled,
  documents,
  onOpenDocument,
  onOpenDocumentInNewWindow,
  onConfigureAI,
  onModelSelectionChange,
  onConversationChange,
  onNotice,
}: ChatScreenProps) {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [conversationId, setConversationId] = useState(initialConversationId);
  const [messages, setMessages] = useState<Message[]>([]);
  const [draft, setDraft] = useState("");
  const [attachments, setAttachments] = useState<ChatAttachment[]>([]);
  const [sending, setSending] = useState(false);
  const [mobileHistoryOpen, setMobileHistoryOpen] = useState(false);
  const controllerRef = useRef<AbortController | null>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const attachmentInputRef = useRef<HTMLInputElement>(null);
  const attachmentGenerationRef = useRef(0);
  const conversationIdRef = useRef(conversationId);
  const skipConversationLoadRef = useRef<string | undefined>(undefined);
  const resumeAttemptedRef = useRef(false);
  const onConversationChangeRef = useRef(onConversationChange);

  useEffect(() => {
    onConversationChangeRef.current = onConversationChange;
  }, [onConversationChange]);

  const loadConversations = useCallback(async () => {
    const response = await fetch(
      `/api/ai/conversations?notebookId=${encodeURIComponent(notebookId)}`,
    );
    if (!response.ok) return;
    const items = (await response.json()) as Conversation[];
    setConversations(items);
    if (!resumeAttemptedRef.current) {
      resumeAttemptedRef.current = true;
      const latest = items[0];
      if (
        !conversationIdRef.current &&
        latest &&
        Date.now() - new Date(latest.updatedAt).getTime() <=
          CHAT_IDLE_CUTOFF_MS
      ) {
        conversationIdRef.current = latest.id;
        setConversationId(latest.id);
        onConversationChangeRef.current(latest.id);
      }
    }
  }, [notebookId]);

  useEffect(() => {
    queueMicrotask(() => void loadConversations());
  }, [loadConversations]);

  const modelOptions = useMemo(() => chatModelOptions(providers), [providers]);
  const activeModel = useMemo(
    () => resolveChatModel(providers, modelSelection),
    [modelSelection, providers],
  );
  const activeModelIndex = modelOptions.findIndex(
    (option) =>
      option.configId === activeModel?.provider.id &&
      option.modelId === activeModel.modelId,
  );
  const modelGroups = useMemo(() => {
    const groups = new Map<
      string,
      {
        configId: string;
        label: string;
        options: { option: (typeof modelOptions)[number]; index: number }[];
      }
    >();
    modelOptions.forEach((option, index) => {
      const current = groups.get(option.configId) ?? {
        configId: option.configId,
        label: option.groupLabel,
        options: [],
      };
      current.options.push({ option, index });
      groups.set(option.configId, current);
    });
    return [...groups.values()];
  }, [modelOptions]);

  useEffect(() => {
    if (!conversationId) {
      queueMicrotask(() => setMessages([]));
      return;
    }
    if (skipConversationLoadRef.current === conversationId) {
      skipConversationLoadRef.current = undefined;
      return;
    }
    const controller = new AbortController();
    fetch(`/api/ai/conversations/${encodeURIComponent(conversationId)}`, {
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) throw new Error(await response.text());
        return (await response.json()) as Message[];
      })
      .then(setMessages)
      .catch((error) => {
        if (!controller.signal.aborted) {
          onNotice(error instanceof Error ? error.message : "无法载入这段对话");
        }
      });
    return () => controller.abort();
  }, [conversationId, onNotice]);

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  useEffect(
    () => () => {
      controllerRef.current?.abort();
    },
    [],
  );

  const selectConversation = useCallback(
    (id?: string) => {
      controllerRef.current?.abort();
      attachmentGenerationRef.current += 1;
      setSending(false);
      setAttachments([]);
      setMobileHistoryOpen(false);
      conversationIdRef.current = id;
      setConversationId(id);
      onConversationChange(id);
    },
    [onConversationChange],
  );

  const queueAttachments = useCallback(
    async (files: readonly File[]) => {
      const generation = attachmentGenerationRef.current;
      try {
        const items = await Promise.all(files.map(toChatAttachment));
        if (generation !== attachmentGenerationRef.current) return;
        setAttachments((current) => [...current, ...items].slice(0, 4));
      } catch {
        if (generation === attachmentGenerationRef.current) {
          onNotice("无法读取这张图片");
        }
      }
    },
    [onNotice],
  );

  const send = useCallback(async () => {
    const text = draft.trim();
    if (
      (!text && attachments.length === 0) ||
      sending ||
      controllerRef.current
    ) {
      return;
    }
    if (providers.length === 0) {
      onConfigureAI();
      onNotice("请先添加 AI 供应商");
      return;
    }
    const controller = new AbortController();
    controllerRef.current = controller;
    setDraft("");
    const sentAttachments = attachments;
    setAttachments([]);
    setSending(true);
    const now = new Date().toISOString();
    const user: Message = {
      id: crypto.randomUUID(),
      conversationId: conversationId ?? "pending",
      role: "user",
      content: text,
      createdAt: now,
      attachments: sentAttachments,
    };
    const assistant: Message = {
      id: crypto.randomUUID(),
      conversationId: conversationId ?? "pending",
      role: "assistant",
      content: "",
      createdAt: now,
      tools: [],
    };
    setMessages((current) => [...current, user, assistant]);
    try {
      const response = await fetch("/api/ai/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        signal: controller.signal,
        body: JSON.stringify({
          conversationId,
          notebookId,
          providerId: activeModel?.provider.id,
          modelId: activeModel?.modelId,
          systemPrompt: chatSystemPrompt,
          semanticSearchEnabled,
          message: text,
          attachments: sentAttachments,
        }),
      });
      await consumeEventStream(response, (event) => {
        if (event.type === "start") {
          if (!conversationId && event.conversationId) {
            skipConversationLoadRef.current = event.conversationId;
            conversationIdRef.current = event.conversationId;
            setConversationId(event.conversationId);
            onConversationChange(event.conversationId);
          }
          setMessages((current) =>
            current.map((message) =>
              message.id === assistant.id
                ? { ...message, sources: event.sources ?? [] }
                : message,
            ),
          );
        } else if (event.type === "text-delta" && event.text) {
          setMessages((current) =>
            current.map((message) =>
              message.id === assistant.id
                ? { ...message, content: message.content + event.text }
                : message,
            ),
          );
        } else if (event.type === "tool-call" && event.toolCallId && event.tool) {
          setMessages((current) =>
            current.map((message) =>
              message.id === assistant.id
                ? {
                    ...message,
                    tools: [
                      ...(message.tools ?? []),
                      {
                        toolCallId: event.toolCallId!,
                        tool: event.tool!,
                        summary: event.summary || "正在查找笔记…",
                        status: "pending",
                        textOffset: Array.from(message.content).length,
                      },
                    ],
                  }
                : message,
            ),
          );
        } else if (
          event.type === "tool-result" &&
          event.toolCallId &&
          event.tool
        ) {
          setMessages((current) =>
            current.map((message) =>
              message.id === assistant.id
                ? {
                    ...message,
                    sources: mergeSources(message.sources, event.sources),
                    tools: (message.tools ?? []).map((activity) =>
                      activity.toolCallId === event.toolCallId
                        ? {
                            ...activity,
                            summary: event.summary || activity.summary,
                            status: event.message ? "error" : "complete",
                            sources: event.sources ?? [],
                          }
                        : activity,
                    ),
                  }
                : message,
            ),
          );
        } else if (event.type === "error") {
          throw new Error(event.message || "AI 回复失败");
        }
      });
      await loadConversations();
    } catch (error) {
      const message = controller.signal.aborted
        ? "已停止。"
        : error instanceof Error
          ? error.message
          : "AI 回复失败";
      setMessages((current) =>
        current.map((item) =>
          item.id === assistant.id
            ? {
                ...item,
                content: item.content || message,
                tools: item.tools?.map((activity) =>
                  activity.status === "pending"
                    ? {
                        ...activity,
                        status: "error" as const,
                        summary: `${activity.summary} · ${message}`,
                      }
                    : activity,
                ),
              }
            : item,
        ),
      );
      if (!controller.signal.aborted) onNotice(message);
    } finally {
      if (controllerRef.current === controller) controllerRef.current = null;
      setSending(false);
    }
  }, [activeModel, attachments, chatSystemPrompt, conversationId, draft, loadConversations, notebookId, onConfigureAI, onConversationChange, onNotice, providers.length, semanticSearchEnabled, sending]);

  const deleteConversation = useCallback(
    async (id: string) => {
      if (conversationId === id) {
        controllerRef.current?.abort();
      }
      const response = await fetch(`/api/ai/conversations/${encodeURIComponent(id)}`, {
        method: "DELETE",
      });
      if (response.ok) {
        if (conversationId === id) selectConversation(undefined);
        await loadConversations();
      }
    },
    [conversationId, loadConversations, selectConversation],
  );

  const activeTitle = useMemo(
    () => conversations.find((item) => item.id === conversationId)?.title,
    [conversationId, conversations],
  );

  return (
    <section className="chat-screen">
      <aside
        className={`chat-history ${mobileHistoryOpen ? "mobile-open" : ""}`}
      >
        <header>
          <strong>对话</strong>
          <button type="button" onClick={() => selectConversation(undefined)} aria-label="新对话">
            <Plus size={16} />
          </button>
        </header>
        {conversations.map((conversation) => (
          <div className={`chat-history-row ${conversation.id === conversationId ? "active" : ""}`} key={conversation.id}>
            <button type="button" onClick={() => selectConversation(conversation.id)}>
              <span>{conversation.title}</span>
              <small>{new Date(conversation.updatedAt).toLocaleDateString("zh-CN")}</small>
            </button>
            <button type="button" onClick={() => void deleteConversation(conversation.id)} aria-label="删除对话">
              <Trash2 size={13} />
            </button>
          </div>
        ))}
      </aside>
      <div className="chat-main">
        <header className="chat-header">
          <div>
            <MessageCircle size={18} />
            <strong>{activeTitle ?? "与笔记对话"}</strong>
          </div>
          <div className="chat-header-actions">
            <button
              type="button"
              className="chat-mobile-action"
              onClick={() => setMobileHistoryOpen((open) => !open)}
              aria-label="对话历史"
            >
              <History size={16} />
            </button>
            <button
              type="button"
              className="chat-mobile-action"
              onClick={() => selectConversation(undefined)}
              aria-label="新对话"
            >
              <Plus size={16} />
            </button>
            <select
              value={activeModelIndex >= 0 ? String(activeModelIndex) : ""}
              onChange={(event) => {
                const option = modelOptions[Number(event.target.value)];
                if (option) {
                  onModelSelectionChange({
                    configId: option.configId,
                    modelId: option.modelId,
                  });
                }
              }}
              aria-label="AI 模型"
            >
              {modelGroups.map((group) => (
                <optgroup label={group.label} key={group.configId}>
                  {group.options.map(({ option, index }) => (
                    <option value={String(index)} key={`${option.configId}:${option.modelId}`}>
                      {option.label}
                    </option>
                  ))}
                </optgroup>
              ))}
            </select>
          </div>
        </header>
        <div
          className="chat-transcript"
          onDragOver={(event) => {
            if (Array.from(event.dataTransfer.types).includes("Files")) {
              event.preventDefault();
            }
          }}
          onDrop={(event) => {
            if (Array.from(event.dataTransfer.types).includes("Files")) {
              event.preventDefault();
            }
            const files = imageFilesFrom(event.dataTransfer);
            if (files.length === 0) return;
            void queueAttachments(files);
          }}
        >
          {messages.length === 0 ? (
            <div className="chat-empty">
              <MessageCircle size={28} />
              <h1>与你的笔记对话</h1>
              <p>我会先检索当前知识库，只引用实际找到的非私密笔记，并用 [[笔记标题]] 标注来源。</p>
              {providers.length === 0 ? (
                <button type="button" onClick={onConfigureAI}>
                  添加 AI 供应商
                </button>
              ) : null}
            </div>
          ) : null}
          {messages.map((message) => (
            <article className={`chat-message ${message.role}`} key={message.id}>
              <strong>{message.role === "user" ? "你" : "墨笺"}</strong>
              {message.attachments && message.attachments.length > 0 ? (
                <div className="chat-message-attachments">
                  {message.attachments.map((attachment) => (
                    <Image
                      src={attachment.dataUrl}
                      alt={attachment.name}
                      key={attachment.id}
                      width={320}
                      height={240}
                      unoptimized
                    />
                  ))}
                </div>
              ) : null}
              {message.sources && message.sources.length > 0 ? (
                <div className="chat-sources">
                  {message.sources.map((source) => (
                    <button
                      type="button"
                      key={source.id}
                      onClick={(event) =>
                        event.metaKey || event.ctrlKey
                          ? onOpenDocumentInNewWindow(source.id)
                          : onOpenDocument(source.id)
                      }
                    >
                      {source.title}
                    </button>
                  ))}
                </div>
              ) : null}
              <ChatMessageBody
                message={message}
                streaming={
                  sending &&
                  message.role === "assistant" &&
                  message.id === messages.at(-1)?.id
                }
                documents={documents}
                onOpenDocument={onOpenDocument}
                onOpenDocumentInNewWindow={onOpenDocumentInNewWindow}
              />
            </article>
          ))}
          <div ref={endRef} />
        </div>
        <div className="chat-composer">
          {attachments.length > 0 ? (
            <div className="chat-attachment-previews">
              {attachments.map((attachment) => (
                <div key={attachment.id}>
                  <Image
                    src={attachment.dataUrl}
                    alt={attachment.name}
                    width={58}
                    height={58}
                    unoptimized
                  />
                  <button
                    type="button"
                    aria-label={`移除 ${attachment.name}`}
                    onClick={() =>
                      setAttachments((current) =>
                        current.filter((item) => item.id !== attachment.id),
                      )
                    }
                  >
                    ×
                  </button>
                </div>
              ))}
            </div>
          ) : null}
          <textarea
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                void send();
              }
            }}
            onPaste={(event) => {
              const files = imageFilesFrom(event.clipboardData);
              if (files.length === 0) return;
              event.preventDefault();
              void queueAttachments(files);
            }}
            placeholder="询问你的笔记…"
            rows={3}
          />
          <input
            ref={attachmentInputRef}
            type="file"
            accept="image/*"
            multiple
            hidden
            onChange={(event) => {
              const files = Array.from(event.target.files ?? []);
              event.target.value = "";
              void queueAttachments(files);
            }}
          />
          <div className="chat-composer-actions">
            <button
              type="button"
              onClick={() => attachmentInputRef.current?.click()}
              disabled={sending || attachments.length >= 4}
              aria-label="添加图片"
            >
              <ImagePlus size={16} />
            </button>
            {sending ? (
              <button type="button" onClick={() => controllerRef.current?.abort()} aria-label="停止生成"><Square size={16} /></button>
            ) : (
              <button type="button" onClick={() => void send()} disabled={!draft.trim() && attachments.length === 0} aria-label="发送"><Send size={17} /></button>
            )}
          </div>
        </div>
      </div>
    </section>
  );
}
