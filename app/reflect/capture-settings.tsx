"use client";

import { useEffect, useState } from "react";
import { Copy, Link2, Plus, Trash2 } from "lucide-react";

interface CaptureToken {
  id: string;
  notebookId: string;
  label: string;
  keyHint: string;
  createdAt: string;
  token?: string;
}

export function CaptureSettings({ notebookId, onNotice }: { notebookId: string; onNotice: (message: string) => void }) {
  const [tokens, setTokens] = useState<CaptureToken[]>([]);
  const [newToken, setNewToken] = useState("");

  useEffect(() => {
    fetch(`/api/capture/tokens?notebookId=${encodeURIComponent(notebookId)}`)
      .then((response) => (response.ok ? response.json() : []))
      .then((items) => setTokens(items as CaptureToken[]));
  }, [notebookId]);

  const create = async () => {
    const response = await fetch("/api/capture/tokens", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ notebookId, label: "浏览器扩展" }),
    });
    if (!response.ok) {
      onNotice(await response.text());
      return;
    }
    const item = (await response.json()) as CaptureToken;
    setNewToken(item.token ?? "");
    setTokens((current) => [item, ...current]);
  };

  const remove = async (id: string) => {
    const response = await fetch(`/api/capture/tokens/${encodeURIComponent(id)}`, { method: "DELETE" });
    if (response.ok) setTokens((current) => current.filter((item) => item.id !== id));
  };

  return (
    <section className="capture-settings-section">
      <h2>浏览器捕获</h2>
      <p className="settings-section-copy">扩展将页面链接、选区与截图排队后发送到本机；服务不可用时保留队列，下次重试。令牌只在创建时显示一次。</p>
      {newToken ? (
        <div className="capture-token-reveal">
          <code>{newToken}</code>
          <button type="button" onClick={() => void navigator.clipboard.writeText(newToken).then(() => onNotice("令牌已复制"))}><Copy size={14} /> 复制</button>
        </div>
      ) : null}
      <div className="provider-list">
        {tokens.map((token) => (
          <div className="provider-row" key={token.id}>
            <Link2 size={17} />
            <span><strong>{token.label}</strong><small>{token.keyHint} · {new Date(token.createdAt).toLocaleDateString("zh-CN")}</small></span>
            <button type="button" className="icon-danger" onClick={() => void remove(token.id)}><Trash2 size={15} /></button>
          </div>
        ))}
      </div>
      <button type="button" className="primary-action" onClick={() => void create()}><Plus size={15} /> 创建扩展令牌</button>
    </section>
  );
}
