"use client";

import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import type { StoredTemplate } from "./templates";

interface TemplateSettingsProps {
  notebookId: string;
  templates: readonly StoredTemplate[];
  onChange: (templates: StoredTemplate[]) => void;
  onNotice: (message: string) => void;
}

export function TemplateSettings({ notebookId, templates, onChange, onNotice }: TemplateSettingsProps) {
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");

  const add = async () => {
    if (!title.trim()) return;
    const response = await fetch("/api/templates", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ notebookId, title, content }),
    });
    if (!response.ok) {
      onNotice(await response.text());
      return;
    }
    const item = (await response.json()) as StoredTemplate;
    onChange([...templates, item]);
    setTitle("");
    setContent("");
    onNotice("模板已添加，可在编辑器中输入 / 调用");
  };

  const remove = async (id: string) => {
    const response = await fetch(`/api/templates/${encodeURIComponent(id)}`, { method: "DELETE" });
    if (response.ok) onChange(templates.filter((template) => template.id !== id));
  };

  return (
    <section className="template-settings-section">
      <h2>模板</h2>
      <p className="settings-section-copy">自定义 Markdown 模板只出现在 Slash 插入菜单中，不进入笔记搜索、任务或 AI 检索。</p>
      <div className="template-list">
        {templates.map((template) => (
          <div key={template.id}>
            <span><strong>{template.title}</strong><small>{template.content.slice(0, 80) || "空模板"}</small></span>
            <button type="button" onClick={() => void remove(template.id)} aria-label="删除模板"><Trash2 size={14} /></button>
          </div>
        ))}
      </div>
      <div className="template-form">
        <input value={title} onChange={(event) => setTitle(event.target.value)} placeholder="模板名称" />
        <textarea value={content} onChange={(event) => setContent(event.target.value)} placeholder="Markdown 模板内容" rows={6} />
        <button type="button" className="primary-action" onClick={() => void add()} disabled={!title.trim()}><Plus size={15} /> 添加模板</button>
      </div>
    </section>
  );
}
