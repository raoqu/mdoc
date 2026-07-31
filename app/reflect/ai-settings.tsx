"use client";

import { useEffect, useMemo, useState } from "react";
import { Check, KeyRound, Plus, RefreshCw, Sparkles, Trash2 } from "lucide-react";
import { AI_PROVIDER_CATALOG, type AiProviderConfig, type AiProviderId } from "./ai";

interface AiSettingsProps {
  providers: readonly AiProviderConfig[];
  chatSystemPrompt: string;
  semanticSearchEnabled: boolean;
  onChange: (providers: AiProviderConfig[]) => void;
  onChatSystemPromptChange: (prompt: string) => void;
  onSemanticSearchChange: (enabled: boolean) => void;
  onNotice: (message: string) => void;
}

const CHAT_SYSTEM_PROMPT_MAX_LENGTH = 20_000;

interface SemanticStatus {
  enabled: boolean;
  available: boolean;
  status: "disabled" | "unavailable" | "idle" | "indexing" | "ready" | "failed";
  model?: string;
  indexed: number;
  total: number;
  message?: string;
}

export function AiSettings({
  providers,
  chatSystemPrompt,
  semanticSearchEnabled,
  onChange,
  onChatSystemPromptChange,
  onSemanticSearchChange,
  onNotice,
}: AiSettingsProps) {
  const [provider, setProvider] = useState<AiProviderId>("openai");
  const info = useMemo(
    () => AI_PROVIDER_CATALOG.find((candidate) => candidate.id === provider)!,
    [provider],
  );
  const [model, setModel] = useState<string>(
    AI_PROVIDER_CATALOG[0].models[0].id,
  );
  const [apiKey, setApiKey] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [adding, setAdding] = useState(false);
  const [semanticStatus, setSemanticStatus] = useState<SemanticStatus | null>(null);

  useEffect(() => {
    let active = true;
    let timer = 0;
    const refresh = async () => {
      try {
        const response = await fetch("/api/semantic");
        if (!response.ok) throw new Error(await response.text());
        const next = (await response.json()) as SemanticStatus;
        if (!active) return;
        setSemanticStatus(next);
        if (
          semanticSearchEnabled &&
          (next.status === "idle" ||
            next.status === "disabled" ||
            next.status === "indexing")
        ) {
          timer = window.setTimeout(() => void refresh(), 600);
        }
      } catch {
        if (active) {
          setSemanticStatus({
            enabled: false,
            available: false,
            status: "unavailable",
            indexed: 0,
            total: 0,
            message: "无法连接本地语义索引服务。",
          });
        }
      }
    };
    void refresh();
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [semanticSearchEnabled]);

  const rebuildSemanticIndex = async () => {
    try {
      const response = await fetch("/api/semantic", { method: "POST" });
      if (!response.ok) throw new Error(await response.text());
      setSemanticStatus((await response.json()) as SemanticStatus);
    } catch (error) {
      onNotice(error instanceof Error ? error.message : "无法重建语义索引");
    }
  };

  const changeProvider = (next: AiProviderId) => {
    setProvider(next);
    const nextInfo = AI_PROVIDER_CATALOG.find((candidate) => candidate.id === next)!;
    setModel(nextInfo.models[0].id);
  };

  const add = async () => {
    if (!apiKey.trim() || !model.trim()) return;
    setAdding(true);
    try {
      const response = await fetch("/api/ai/providers", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          provider,
          label: info.label,
          model,
          apiKey,
          baseUrl,
          makeDefault: providers.length === 0,
        }),
      });
      if (!response.ok) throw new Error(await response.text());
      onChange((await response.json()) as AiProviderConfig[]);
      setApiKey("");
      setBaseUrl("");
      onNotice("AI 供应商已添加，密钥只保存在系统钥匙串中");
    } catch (error) {
      onNotice(error instanceof Error ? error.message : "无法添加供应商");
    } finally {
      setAdding(false);
    }
  };

  const makeDefault = async (id: string) => {
    const response = await fetch(`/api/ai/providers/${encodeURIComponent(id)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ makeDefault: true }),
    });
    if (response.ok) onChange((await response.json()) as AiProviderConfig[]);
  };

  const remove = async (id: string) => {
    const response = await fetch(`/api/ai/providers/${encodeURIComponent(id)}`, { method: "DELETE" });
    if (response.ok) onChange(providers.filter((candidate) => candidate.id !== id));
  };

  return (
    <section className="ai-settings-section">
      <h2>AI 供应商</h2>
      <p className="settings-section-copy">直接使用你自己的 API 密钥。密钥进入操作系统钥匙串，不写入浏览器、知识库或 Markdown。</p>
      <div className="provider-list">
        {providers.map((item) => (
          <div className="provider-row" key={item.id}>
            <KeyRound size={17} />
            <span><strong>{item.label}</strong><small>{item.model} · {item.keyHint}</small></span>
            {item.isDefault ? <em><Check size={13} /> 默认</em> : <button type="button" onClick={() => void makeDefault(item.id)}>设为默认</button>}
            <button type="button" className="icon-danger" onClick={() => void remove(item.id)} aria-label="移除供应商"><Trash2 size={15} /></button>
          </div>
        ))}
      </div>
      <div className="provider-form">
        <select value={provider} onChange={(event) => changeProvider(event.target.value as AiProviderId)} aria-label="供应商">
          {AI_PROVIDER_CATALOG.map((candidate) => <option value={candidate.id} key={candidate.id}>{candidate.label}</option>)}
        </select>
        <input
          value={model}
          onChange={(event) => setModel(event.target.value)}
          list={`ai-models-${provider}`}
          placeholder="模型 ID"
          aria-label="模型"
        />
        <datalist id={`ai-models-${provider}`}>
          {info.models.map((candidate) => (
            <option value={candidate.id} key={candidate.id}>
              {candidate.label}
            </option>
          ))}
        </datalist>
        <input type="password" value={apiKey} onChange={(event) => setApiKey(event.target.value)} placeholder={info.keyPlaceholder} aria-label="API 密钥" autoComplete="off" />
        <input type="url" value={baseUrl} onChange={(event) => setBaseUrl(event.target.value)} placeholder="自定义 Base URL（可选）" aria-label="自定义 Base URL" />
        <button type="button" className="primary-action" onClick={() => void add()} disabled={!apiKey.trim() || adding}><Plus size={15} /> {adding ? "保存中…" : "添加"}</button>
      </div>
      <h2>AI Chat</h2>
      <p className="settings-section-copy">
        下面的附加指令会随每次对话发送；笔记检索、精确引用和隐私规则始终优先。
      </p>
      <div className="setting-row semantic-setting-row">
        <span>
          <strong>本地语义检索</strong>
          <small>
            按含义而不只是关键词检索笔记。句向量和索引完全在本机生成，私密笔记不会进入索引。
          </small>
          {semanticStatus?.status === "indexing" ? (
            <span className="semantic-progress">
              <progress
                max={Math.max(semanticStatus.total, 1)}
                value={semanticStatus.indexed}
                aria-label="语义索引进度"
              />
              <small>
                正在建立索引：{semanticStatus.indexed}/{semanticStatus.total}
              </small>
            </span>
          ) : semanticStatus?.status === "ready" ? (
            <small className="semantic-ready">
              已就绪 · {semanticStatus.model} · {semanticStatus.indexed} 篇
            </small>
          ) : semanticStatus?.status === "failed" ||
            semanticStatus?.status === "unavailable" ? (
            <small className="semantic-error">{semanticStatus.message}</small>
          ) : null}
        </span>
        <div className="semantic-setting-actions">
          {semanticSearchEnabled &&
          semanticStatus?.status !== "unavailable" ? (
            <>
              <button
                type="button"
                onClick={() => void rebuildSemanticIndex()}
                aria-label="重建语义索引"
              >
                <RefreshCw size={13} />
              </button>
              <button
                type="button"
                onClick={() => onSemanticSearchChange(false)}
              >
                关闭
              </button>
            </>
          ) : (
            <button
              type="button"
              disabled={semanticStatus?.available === false}
              onClick={() => onSemanticSearchChange(true)}
            >
              <Sparkles size={13} /> 启用
            </button>
          )}
        </div>
      </div>
      <textarea
        value={chatSystemPrompt}
        onChange={(event) =>
          onChatSystemPromptChange(
            event.target.value.slice(0, CHAT_SYSTEM_PROMPT_MAX_LENGTH),
          )
        }
        maxLength={CHAT_SYSTEM_PROMPT_MAX_LENGTH}
        rows={6}
        placeholder="回答简洁一些；质疑我的假设，并在信息不足时明确指出。"
        aria-label="Chat 系统提示词"
      />
    </section>
  );
}
