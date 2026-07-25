"use client";

import { useEffect, useState } from "react";
import { Cloud, CloudOff, RefreshCw, Unplug } from "lucide-react";

export interface SyncConfig {
  notebookId?: string;
  remoteUrl?: string;
  branch?: string;
  status: "disconnected" | "backing_up" | "backed_up" | "offline" | "needs_review" | "failed";
  lastError?: string;
  lastSyncAt?: string;
  autoSync?: boolean;
}

const STATUS_LABEL: Record<SyncConfig["status"], string> = {
  disconnected: "未连接",
  backing_up: "正在备份",
  backed_up: "已备份",
  offline: "离线，修改已在本机排队",
  needs_review: "需要检查冲突",
  failed: "备份失败",
};

interface SyncSettingsProps {
  notebookId: string;
  config: SyncConfig;
  onChange: (config: SyncConfig) => void;
  onSynced: () => void;
  onNotice: (message: string) => void;
}

export function SyncSettings({ notebookId, config, onChange, onSynced, onNotice }: SyncSettingsProps) {
  const [remoteUrl, setRemoteUrl] = useState("");
  const [branch, setBranch] = useState("main");
  const [token, setToken] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [working, setWorking] = useState(false);

  useEffect(() => {
    fetch(`/api/sync?notebookId=${encodeURIComponent(notebookId)}`)
      .then((response) => response.json())
      .then((next) => {
        const value = next as SyncConfig;
        onChange(value);
        if (value.remoteUrl) setRemoteUrl(value.remoteUrl);
        if (value.branch) setBranch(value.branch);
      });
  }, [notebookId, onChange]);

  const run = async () => {
    setWorking(true);
    onChange({ ...config, status: "backing_up" });
    const response = await fetch(`/api/sync/run?notebookId=${encodeURIComponent(notebookId)}`, { method: "POST" });
    if (!response.ok) {
      const message = await response.text();
      onChange({ ...config, status: "failed", lastError: message });
      onNotice(message);
    } else {
      onChange((await response.json()) as SyncConfig);
      onSynced();
      onNotice("知识库已备份并同步");
    }
    setWorking(false);
  };

  const connect = async () => {
    setWorking(true);
    const response = await fetch("/api/sync", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ notebookId, remoteUrl, branch, token, autoSync: true, confirmPrivateBackup: confirmed }),
    });
    if (!response.ok) {
      onNotice(await response.text());
      setWorking(false);
      return;
    }
    onChange((await response.json()) as SyncConfig);
    setToken("");
    await run();
  };

  const disconnect = async () => {
    const response = await fetch(`/api/sync?notebookId=${encodeURIComponent(notebookId)}`, { method: "DELETE" });
    if (response.ok) onChange({ status: "disconnected" });
  };

  const connected = config.status !== "disconnected" && Boolean(config.remoteUrl);
  return (
    <section className="sync-settings-section">
      <h2>备份与同步</h2>
      <p className="settings-section-copy">以 Markdown 和附件建立 Git 历史；修改会自动提交、拉取、合并并推送。冲突作为可审查的标准标记保留，不会阻塞其他笔记。</p>
      {connected ? (
        <div className="sync-connected-card">
          {config.status === "offline" || config.status === "failed" ? <CloudOff size={19} /> : <Cloud size={19} />}
          <span><strong>{STATUS_LABEL[config.status]}</strong><small>{config.remoteUrl}{config.lastSyncAt ? ` · ${new Date(config.lastSyncAt).toLocaleString("zh-CN")}` : ""}</small>{config.lastError ? <em>{config.lastError}</em> : null}</span>
          <button type="button" onClick={() => void run()} disabled={working}><RefreshCw size={14} /> 立即同步</button>
          <button type="button" onClick={() => void disconnect()}><Unplug size={14} /> 停止</button>
        </div>
      ) : (
        <div className="sync-form">
          <input value={remoteUrl} onChange={(event) => setRemoteUrl(event.target.value)} placeholder="Git 远端（HTTPS、SSH 或本地路径）" />
          <input value={branch} onChange={(event) => setBranch(event.target.value)} placeholder="分支" />
          <input type="password" value={token} onChange={(event) => setToken(event.target.value)} placeholder="HTTPS 令牌（SSH/本地路径留空）" autoComplete="off" />
          <label><input type="checkbox" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} /> 我了解备份历史会包含 private: true 笔记及已删除内容的旧版本，远端可见性由我负责。</label>
          <button type="button" className="primary-action" onClick={() => void connect()} disabled={!remoteUrl.trim() || !confirmed || working}>{working ? "连接中…" : "连接并备份"}</button>
        </div>
      )}
    </section>
  );
}
