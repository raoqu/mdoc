"use client";

import { useEffect, useMemo, useState } from "react";
import { Check, Download, ExternalLink, KeyRound, ListFilter, Plus, RefreshCw, Search, Sparkles, Trash2, X } from "lucide-react";
import { AI_PROVIDER_CATALOG, type AiProviderConfig, type AiProviderId } from "./ai";

interface AiSettingsProps {
  section: "models" | "ai-chat";
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
  status: "disabled" | "unavailable" | "idle" | "downloading" | "indexing" | "ready" | "failed";
  model?: string;
  indexed: number;
  total: number;
  message?: string;
  modelDownload: {
    status: "missing" | "probing" | "downloading" | "verifying" | "installed" | "failed";
    source?: string;
    sourceLabel?: string;
    downloaded: number;
    total: number;
    bytesPerSecond?: number;
    currentFile?: string;
    fileIndex?: number;
    fileTotal?: number;
    target: string;
    message?: string;
    sources: Array<{ id: string; label: string; url: string }>;
  };
}

interface RemoteModelOption {
  id: string;
  label: string;
}

interface ModelPickerProps {
  providerLabel: string;
  models: readonly RemoteModelOption[];
  selected: string;
  loading: boolean;
  error: string;
  onSelect: (model: string) => void;
  onRetry: () => void;
  onCancel: () => void;
  onConfirm: () => void;
}

function ModelPicker({
  providerLabel,
  models,
  selected,
  loading,
  error,
  onSelect,
  onRetry,
  onCancel,
  onConfirm,
}: ModelPickerProps) {
  const [query, setQuery] = useState("");
  const filteredModels = useMemo(() => {
    const folded = query.trim().toLocaleLowerCase();
    if (!folded) return models;
    return models.filter((candidate) =>
      `${candidate.id} ${candidate.label}`.toLocaleLowerCase().includes(folded),
    );
  }, [models, query]);

  return (
    <div
      className="model-picker-backdrop"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onCancel();
      }}
    >
      <div
        className="model-picker-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="model-picker-title"
        onKeyDown={(event) => {
          if (event.key === "Escape") onCancel();
        }}
      >
        <header>
          <span>
            <strong id="model-picker-title">选择 {providerLabel} 模型</strong>
            <small>已选中 {selected ? 1 : 0} 个</small>
          </span>
          <button type="button" onClick={onCancel} aria-label="关闭模型选择器">
            <X size={17} />
          </button>
        </header>
        <label className="model-picker-search">
          <Search size={16} />
          <input
            autoFocus
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && selected) {
                event.preventDefault();
                onConfirm();
              }
            }}
            placeholder="搜索模型 ID 或名称"
            aria-label="搜索模型"
          />
        </label>
        <div className="model-picker-results" role="listbox" aria-label={`${providerLabel} 模型`}>
          {loading ? (
            <div className="model-picker-message">
              <RefreshCw className="spin" size={18} />
              正在从供应商获取模型列表…
            </div>
          ) : error ? (
            <div className="model-picker-message model-picker-error">
              <span>{error}</span>
              <button type="button" onClick={onRetry}>重试</button>
            </div>
          ) : filteredModels.length ? (
            filteredModels.map((candidate) => (
              <button
                type="button"
                role="option"
                aria-selected={selected === candidate.id}
                className={selected === candidate.id ? "selected" : ""}
                key={candidate.id}
                onClick={() => onSelect(candidate.id)}
              >
                <span className="model-picker-choice" aria-hidden="true">
                  {selected === candidate.id ? <Check size={12} /> : null}
                </span>
                <span>
                  <strong>{candidate.id}</strong>
                  {candidate.label !== candidate.id ? <small>{candidate.label}</small> : null}
                </span>
              </button>
            ))
          ) : (
            <div className="model-picker-message">
              {models.length ? "没有匹配的模型" : "供应商没有返回可用于对话的模型"}
            </div>
          )}
        </div>
        <footer>
          <small>单击一行进行单选，也可以关闭后手动填写模型 ID。</small>
          <button type="button" onClick={onCancel}>取消</button>
          <button type="button" className="primary" disabled={!selected || loading} onClick={onConfirm}>
            确认
          </button>
        </footer>
      </div>
    </div>
  );
}

const formatBytes = (bytes: number) => {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / 1024 ** unit).toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
};

export function AiSettings({
  section,
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
  const [modelPickerOpen, setModelPickerOpen] = useState(false);
  const [modelOptions, setModelOptions] = useState<RemoteModelOption[]>([]);
  const [modelSelection, setModelSelection] = useState("");
  const [modelsLoading, setModelsLoading] = useState(false);
  const [modelsError, setModelsError] = useState("");
  const [semanticStatus, setSemanticStatus] = useState<SemanticStatus | null>(null);
  const [semanticModelSource, setSemanticModelSource] = useState("auto");
  const [semanticConnectionError, setSemanticConnectionError] = useState("");
  const [semanticRefreshAttempt, setSemanticRefreshAttempt] = useState(0);

  useEffect(() => {
    if (section !== "ai-chat") return;

    let active = true;
    let timer = 0;
    const refresh = async () => {
      try {
        const response = await fetch("/api/semantic");
        if (!response.ok) throw new Error(await response.text());
        const next = (await response.json()) as SemanticStatus;
        if (!active) return;
        setSemanticConnectionError("");
        setSemanticStatus(next);
        if (
          next.status === "downloading" ||
          (semanticSearchEnabled &&
            (next.status === "idle" ||
              next.status === "disabled" ||
              next.status === "indexing"))
        ) {
          timer = window.setTimeout(() => void refresh(), 600);
        }
      } catch {
        if (active) {
          setSemanticConnectionError(
            "无法连接本地语义索引服务。请确认本地 Go 服务仍在运行。",
          );
          timer = window.setTimeout(() => void refresh(), 1500);
        }
      }
    };
    void refresh();
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [section, semanticRefreshAttempt, semanticSearchEnabled]);

  const rebuildSemanticIndex = async () => {
    try {
      const response = await fetch("/api/semantic", { method: "POST" });
      if (!response.ok) throw new Error(await response.text());
      setSemanticConnectionError("");
      setSemanticStatus((await response.json()) as SemanticStatus);
    } catch (error) {
      onNotice(
        error instanceof TypeError
          ? "无法连接本地语义索引服务，请确认本地 Go 服务仍在运行"
          : error instanceof Error
            ? error.message
            : "无法重建语义索引",
      );
    }
  };

  const downloadSemanticModel = async () => {
    try {
      const response = await fetch("/api/semantic/model", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ source: semanticModelSource, enable: true }),
      });
      if (!response.ok) throw new Error(await response.text());
      setSemanticConnectionError("");
      setSemanticStatus((await response.json()) as SemanticStatus);
      onSemanticSearchChange(true);
      onNotice("模型下载已开始；完成后会自动建立语义索引");
    } catch (error) {
      onNotice(
        error instanceof TypeError
          ? "无法连接本地语义索引服务，请确认本地 Go 服务仍在运行"
          : error instanceof Error
            ? error.message
            : "无法下载语义模型",
      );
    }
  };

  const changeProvider = (next: AiProviderId) => {
    setProvider(next);
    const nextInfo = AI_PROVIDER_CATALOG.find((candidate) => candidate.id === next)!;
    setModel(nextInfo.models[0].id);
    setModelPickerOpen(false);
    setModelOptions([]);
    setModelSelection("");
    setModelsError("");
  };

  const loadModels = async () => {
    if (!apiKey.trim()) {
      onNotice("请先填写 API 密钥，再获取模型列表");
      return;
    }
    setModelPickerOpen(true);
    setModelsLoading(true);
    setModelsError("");
    try {
      const response = await fetch("/api/ai/models", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ provider, apiKey, baseUrl }),
      });
      if (!response.ok) throw new Error((await response.text()).trim());
      const payload = (await response.json()) as { models: RemoteModelOption[] };
      setModelOptions(payload.models);
      setModelSelection(
        payload.models.some((candidate) => candidate.id === model) ? model : "",
      );
    } catch (error) {
      setModelOptions([]);
      setModelSelection("");
      setModelsError(error instanceof Error ? error.message : "无法获取模型列表");
    } finally {
      setModelsLoading(false);
    }
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
      {section === "models" ? (
        <>
          <h2>供应商与模型</h2>
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
            <div className="provider-model-field">
              <input
                value={model}
                onChange={(event) => setModel(event.target.value)}
                list={`ai-models-${provider}`}
                placeholder="模型 ID"
                aria-label="模型"
              />
              <button
                type="button"
                onClick={() => void loadModels()}
                disabled={!apiKey.trim()}
                title={apiKey.trim() ? `从 ${info.label} 获取模型列表` : "请先填写 API 密钥"}
              >
                <ListFilter size={14} /> 获取模型
              </button>
            </div>
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
        </>
      ) : (
        <>
          <h2>对话与检索</h2>
          <p className="settings-section-copy">
            下面的附加指令会随每次对话发送；笔记检索、精确引用和隐私规则始终优先。
          </p>
          <div className="setting-row semantic-setting-row">
        <span>
          <strong>本地语义搜索</strong>
          <small>
            在全局搜索和 AI Chat
            中按含义检索笔记。句向量和索引完全在本机生成，私密笔记不会进入索引。
          </small>
          {semanticStatus?.modelDownload.sources.length ? (
            <small className="semantic-model-sources">
              模型源：{semanticStatus.modelDownload.sources.map((source, index) => (
                <span key={source.id}>
                  {index > 0 ? " · " : ""}
                  <a href={source.url} target="_blank" rel="noreferrer">
                    {source.label}<ExternalLink size={10} />
                  </a>
                </span>
              ))}
            </small>
          ) : null}
          {semanticConnectionError ? (
            <small className="semantic-error">{semanticConnectionError}</small>
          ) : semanticStatus?.status === "downloading" ? (
            <span className="semantic-progress">
              <progress
                max={Math.max(semanticStatus.modelDownload.total, 1)}
                value={semanticStatus.modelDownload.downloaded}
                aria-label="语义模型下载进度"
              />
              <small>
                {semanticStatus.modelDownload.status === "probing"
                  ? "正在探测并选择更快的下载源…"
                  : semanticStatus.modelDownload.status === "verifying"
                    ? "正在校验模型文件…"
                    : `正在从 ${semanticStatus.modelDownload.sourceLabel ?? "模型源"} 下载：${formatBytes(semanticStatus.modelDownload.downloaded)} / ${formatBytes(semanticStatus.modelDownload.total)}${semanticStatus.modelDownload.bytesPerSecond ? ` · ${formatBytes(semanticStatus.modelDownload.bytesPerSecond)}/s` : ""}`}
              </small>
              {semanticStatus.modelDownload.currentFile ? (
                <small>
                  {semanticStatus.modelDownload.fileIndex}/{semanticStatus.modelDownload.fileTotal} · {semanticStatus.modelDownload.currentFile}
                </small>
              ) : null}
            </span>
          ) : semanticStatus?.status === "indexing" ? (
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
          {semanticConnectionError ? (
            <button
              type="button"
              onClick={() => setSemanticRefreshAttempt((attempt) => attempt + 1)}
            >
              <RefreshCw size={13} /> 重试连接
            </button>
          ) : !semanticStatus ? (
            <button type="button" disabled>
              <RefreshCw size={13} /> 正在连接…
            </button>
          ) : semanticStatus.status === "downloading" ? (
            <button type="button" disabled>
              <Download size={13} /> 下载中…
            </button>
          ) : semanticStatus?.available === false ? (
            <>
              <select
                value={semanticModelSource}
                onChange={(event) => setSemanticModelSource(event.target.value)}
                aria-label="语义模型下载源"
              >
                <option value="auto">自动选择</option>
                {semanticStatus.modelDownload.sources.map((source) => (
                  <option value={source.id} key={source.id}>{source.label}</option>
                ))}
              </select>
              <button type="button" onClick={() => void downloadSemanticModel()}>
                <Download size={13} /> 下载并启用
              </button>
            </>
          ) : semanticSearchEnabled ? (
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
        </>
      )}
      {section === "models" && modelPickerOpen ? (
        <ModelPicker
          providerLabel={info.label}
          models={modelOptions}
          selected={modelSelection}
          loading={modelsLoading}
          error={modelsError}
          onSelect={setModelSelection}
          onRetry={() => void loadModels()}
          onCancel={() => setModelPickerOpen(false)}
          onConfirm={() => {
            setModel(modelSelection);
            setModelPickerOpen(false);
          }}
        />
      ) : null}
    </section>
  );
}
