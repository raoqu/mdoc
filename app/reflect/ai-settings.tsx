"use client";

import { useMemo, useState } from "react";
import { Check, KeyRound, Plus, Trash2 } from "lucide-react";
import { AI_PROVIDER_CATALOG, type AiProviderConfig, type AiProviderId } from "./ai";

interface AiSettingsProps {
  providers: readonly AiProviderConfig[];
  onChange: (providers: AiProviderConfig[]) => void;
  onNotice: (message: string) => void;
}

export function AiSettings({ providers, onChange, onNotice }: AiSettingsProps) {
  const [provider, setProvider] = useState<AiProviderId>("openai");
  const info = useMemo(
    () => AI_PROVIDER_CATALOG.find((candidate) => candidate.id === provider)!,
    [provider],
  );
  const [model, setModel] = useState<string>(AI_PROVIDER_CATALOG[0].models[0]);
  const [apiKey, setApiKey] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [adding, setAdding] = useState(false);

  const changeProvider = (next: AiProviderId) => {
    setProvider(next);
    const nextInfo = AI_PROVIDER_CATALOG.find((candidate) => candidate.id === next)!;
    setModel(nextInfo.models[0]);
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
      <p className="settings-section-copy">直接使用你自己的 API 密钥。密钥进入操作系统钥匙串，不写入浏览器、数据库或 Markdown。</p>
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
        <select value={model} onChange={(event) => setModel(event.target.value)} aria-label="模型">
          {info.models.map((candidate) => <option value={candidate} key={candidate}>{candidate}</option>)}
        </select>
        <input type="password" value={apiKey} onChange={(event) => setApiKey(event.target.value)} placeholder={info.keyPlaceholder} aria-label="API 密钥" autoComplete="off" />
        <input type="url" value={baseUrl} onChange={(event) => setBaseUrl(event.target.value)} placeholder="自定义 Base URL（可选）" aria-label="自定义 Base URL" />
        <button type="button" className="primary-action" onClick={() => void add()} disabled={!apiKey.trim() || adding}><Plus size={15} /> {adding ? "保存中…" : "添加"}</button>
      </div>
    </section>
  );
}
