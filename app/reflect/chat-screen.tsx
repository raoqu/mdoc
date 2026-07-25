"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { MessageCircle, Plus, Send, Square, Trash2 } from "lucide-react";
import { consumeEventStream, type AiProviderConfig } from "./ai";
import type { DocumentLocation } from "./types";

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
  sources?: { id: string; title: string }[];
}

interface ChatScreenProps {
  notebookId: string;
  initialConversationId?: string;
  providers: readonly AiProviderConfig[];
  documents: readonly DocumentLocation[];
  onOpenDocument: (documentId: string) => void;
  onConfigureAI: () => void;
  onConversationChange: (conversationId?: string) => void;
  onNotice: (message: string) => void;
}

function LinkedText({
  text,
  documents,
  onOpenDocument,
}: {
  text: string;
  documents: readonly DocumentLocation[];
  onOpenDocument: (documentId: string) => void;
}) {
  return text.split(/(\[\[[^\]]+\]\])/g).map((part, index) => {
    const match = part.match(/^\[\[([^\]]+)\]\]$/);
    if (!match) return <span key={index}>{part}</span>;
    const target = match[1].split("#", 1)[0].trim().toLocaleLowerCase();
    const note = documents.find(
      ({ document }) =>
        document.title.toLocaleLowerCase() === target ||
        document.aliases?.some((alias) => alias.toLocaleLowerCase() === target),
    );
    return note ? (
      <button
        type="button"
        className="chat-wikilink"
        key={index}
        onClick={() => onOpenDocument(note.document.id)}
      >
        {part}
      </button>
    ) : (
      <span key={index}>{part}</span>
    );
  });
}

export function ChatScreen({
  notebookId,
  initialConversationId,
  providers,
  documents,
  onOpenDocument,
  onConfigureAI,
  onConversationChange,
  onNotice,
}: ChatScreenProps) {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [conversationId, setConversationId] = useState(initialConversationId);
  const [messages, setMessages] = useState<Message[]>([]);
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [providerId, setProviderId] = useState("");
  const controllerRef = useRef<AbortController | null>(null);
  const endRef = useRef<HTMLDivElement>(null);

  const loadConversations = useCallback(async () => {
    const response = await fetch(
      `/api/ai/conversations?notebookId=${encodeURIComponent(notebookId)}`,
    );
    if (response.ok) setConversations((await response.json()) as Conversation[]);
  }, [notebookId]);

  useEffect(() => {
    queueMicrotask(() => void loadConversations());
  }, [loadConversations]);

  const preferredProvider =
    providers.find((provider) => provider.isDefault) ?? providers[0];
  const effectiveProviderId = providers.some(
    (provider) => provider.id === providerId,
  )
    ? providerId
    : preferredProvider?.id ?? "";

  useEffect(() => {
    if (!conversationId) {
      queueMicrotask(() => setMessages([]));
      return;
    }
    fetch(`/api/ai/conversations/${encodeURIComponent(conversationId)}`)
      .then(async (response) => {
        if (!response.ok) throw new Error(await response.text());
        return (await response.json()) as Message[];
      })
      .then(setMessages)
      .catch(() => onNotice("无法载入这段对话"));
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
      setSending(false);
      setConversationId(id);
      onConversationChange(id);
    },
    [onConversationChange],
  );

  const send = useCallback(async () => {
    const text = draft.trim();
    if (!text || sending) return;
    if (providers.length === 0) {
      onConfigureAI();
      onNotice("请先添加 AI 供应商");
      return;
    }
    const controller = new AbortController();
    controllerRef.current = controller;
    setDraft("");
    setSending(true);
    const now = new Date().toISOString();
    const user: Message = {
      id: crypto.randomUUID(),
      conversationId: conversationId ?? "pending",
      role: "user",
      content: text,
      createdAt: now,
    };
    const assistant: Message = {
      id: crypto.randomUUID(),
      conversationId: conversationId ?? "pending",
      role: "assistant",
      content: "",
      createdAt: now,
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
          providerId: effectiveProviderId,
          message: text,
        }),
      });
      await consumeEventStream(response, (event) => {
        if (event.type === "start") {
          if (!conversationId && event.conversationId) {
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
        } else if (event.type === "error") {
          throw new Error(event.message || "AI 回复失败");
        }
      });
      await loadConversations();
    } catch (error) {
      if (!controller.signal.aborted) {
        onNotice(error instanceof Error ? error.message : "AI 回复失败");
      }
    } finally {
      if (controllerRef.current === controller) controllerRef.current = null;
      setSending(false);
    }
  }, [conversationId, draft, effectiveProviderId, loadConversations, notebookId, onConfigureAI, onConversationChange, onNotice, providers.length, sending]);

  const deleteConversation = useCallback(
    async (id: string) => {
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
      <aside className="chat-history">
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
          <select value={effectiveProviderId} onChange={(event) => setProviderId(event.target.value)} aria-label="AI 模型">
            {providers.map((provider) => (
              <option value={provider.id} key={provider.id}>{provider.label} · {provider.model}</option>
            ))}
          </select>
        </header>
        <div className="chat-transcript">
          {messages.length === 0 ? (
            <div className="chat-empty">
              <MessageCircle size={28} />
              <h1>与你的笔记对话</h1>
              <p>我会先检索当前知识库，只引用实际找到的非私密笔记，并用 [[笔记标题]] 标注来源。</p>
            </div>
          ) : null}
          {messages.map((message) => (
            <article className={`chat-message ${message.role}`} key={message.id}>
              <strong>{message.role === "user" ? "你" : "墨笺"}</strong>
              {message.sources && message.sources.length > 0 ? (
                <div className="chat-sources">
                  {message.sources.map((source) => (
                    <button type="button" key={source.id} onClick={() => onOpenDocument(source.id)}>
                      {source.title}
                    </button>
                  ))}
                </div>
              ) : null}
              <div className="chat-message-body">
                <LinkedText text={message.content || (sending ? "正在查找相关笔记…" : "")} documents={documents} onOpenDocument={onOpenDocument} />
              </div>
            </article>
          ))}
          <div ref={endRef} />
        </div>
        <div className="chat-composer">
          <textarea
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                void send();
              }
            }}
            placeholder="询问你的笔记…"
            rows={3}
          />
          {sending ? (
            <button type="button" onClick={() => controllerRef.current?.abort()} aria-label="停止生成"><Square size={16} /></button>
          ) : (
            <button type="button" onClick={() => void send()} disabled={!draft.trim()} aria-label="发送"><Send size={17} /></button>
          )}
        </div>
      </div>
    </section>
  );
}
