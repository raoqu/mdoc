"use client";

import { useCallback, useEffect, useState } from "react";
import { Download, ExternalLink, Globe, Play, Save } from "lucide-react";
import { formatPublicationTarget, parsePublicationTarget, type PublicationTarget } from "./publication-target";
import {
  publicationResponseJSON as responseJSON,
  type PublicationConfig,
  type PublicationStatus,
  type PublicationDeployInput,
  type PublicationDeployment,
} from "./publication-api";

export function PublicationSettings({ knowledgeBaseLabel, pendingSave, publishing, onPublish, onNotice }: {
  knowledgeBaseLabel: string;
  pendingSave: boolean;
  publishing: boolean;
  onPublish: (input?: PublicationDeployInput) => Promise<PublicationDeployment>;
  onNotice: (message: string) => void;
}) {
  const [status, setStatus] = useState<PublicationStatus | null>(null);
  const [config, setConfig] = useState<PublicationConfig | null>(null);
  const [target, setTarget] = useState<PublicationTarget | null>(null);
  const [working, setWorking] = useState("");
  const [log, setLog] = useState("");
  const [built, setBuilt] = useState(false);
  const refresh = useCallback(async () => {
    const next = await responseJSON<PublicationStatus>(await fetch("/api/publication"));
    setStatus(next);
    setConfig(next.config);
    setTarget(parsePublicationTarget(next.config.deployTarget));
  }, []);

  useEffect(() => {
    let active = true;
    fetch("/api/publication")
      .then(responseJSON<PublicationStatus>)
      .then((next) => {
        if (active) {
          // Preserve the original scope if another window switches knowledge bases.
          // The save request will then be rejected instead of writing edits to it.
          setStatus((current) => current && current.directory !== next.directory ? current : next);
          // A note finishing its save must not overwrite edits to this form.
          setConfig((current) => current ?? next.config);
          setTarget((current) => current ?? parsePublicationTarget(next.config.deployTarget));
        }
      })
      .catch((error: unknown) => { if (active) setLog(String(error)); });
    return () => { active = false; };
  }, [pendingSave]);

  const run = async (action: "save" | "theme" | "build" | "deploy") => {
    if (!config || !status || !target || working || pendingSave || publishing) return;
    setWorking(action);
    setLog("");
    let noticeReportedByWorkspace = false;
    try {
      const deployTarget = formatPublicationTarget(target);
      if (action === "deploy") {
        noticeReportedByWorkspace = true;
        const deployment = await onPublish({ directory: status.directory, config: { ...config, deployTarget } });
        setStatus(deployment.status);
        setConfig(deployment.status.config);
        setTarget(parsePublicationTarget(deployment.status.config.deployTarget));
        setLog(deployment.result.log);
        setBuilt(true);
        return;
      }
      const next = await responseJSON<PublicationStatus>(await fetch("/api/publication", {
        method: "PUT", headers: { "Content-Type": "application/json", "X-Mdoc-Publication-Directory": status.directory }, body: JSON.stringify({ ...config, deployTarget }),
      }));
      setStatus(next);
      setConfig(next.config);
      setTarget(parsePublicationTarget(next.config.deployTarget));
      if (action !== "save") {
        const result = await responseJSON<{ log: string; documents?: number }>(await fetch(`/api/publication/${action}`, { method: "POST", headers: { "X-Mdoc-Publication-Directory": status.directory } }));
        setLog(result.log);
        if (action === "theme") setStatus({ ...next, themeInstalled: true });
        if (action === "build") setBuilt(true);
        onNotice(action === "theme" ? "主题安装完成" : "站点构建完成");
      } else onNotice("发布配置已保存");
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      setLog(message);
      if (!noticeReportedByWorkspace) onNotice(`发布操作失败：${message.split("\n")[0].slice(0, 160)}`);
    } finally { setWorking(""); }
  };

  if (!config || !status || !target) return <section><p role="status">{log || "正在读取发布配置…"}</p>{log ? <button type="button" onClick={() => void refresh().catch((error: unknown) => setLog(String(error)))}>重试</button> : null}</section>;
  const disabled = Boolean(working) || pendingSave || publishing;
  const selectedTheme = status.themes?.find((theme) => theme.id === config.theme);
  const installed = Boolean(selectedTheme && !selectedTheme.module) || (status.themeInstalled && (config.theme ?? "") === (status.config.theme ?? "") && config.themeModule === status.config.themeModule && config.themeVersion === status.config.themeVersion);
  const field = (key: keyof PublicationConfig, value: string | boolean) => setConfig({ ...config, [key]: value });

  return <section className="publication-settings">
    <h2>{knowledgeBaseLabel} · 发布配置</h2>
    <p className="settings-section-copy">配置仅用于此知识库。每个知识库可设置自己的服务器和发布目录。</p>
    <p className="settings-section-copy">在笔记右侧勾选“标记为发布”，或在 Markdown frontmatter 中设置 <code>publish: true</code>。只发布当前知识库中已保存的标记文档；私密、回收站和 <code>draft: true</code> 文档始终排除。取消标记后再次部署即可撤下。</p>
    <fieldset disabled={disabled}>
      <legend>发布服务器</legend>
      <label>Server IP<input value={target.serverIP} onChange={(event) => setTarget({ ...target, serverIP: event.target.value })} placeholder="10.6.0.1" autoCapitalize="none" spellCheck={false} aria-describedby="publication-server-help" /></label>
      <p id="publication-server-help" className="settings-section-copy">填写 IPv4 地址或主机名，不含 http://、用户名或端口。</p>
      <label>SSH 用户<input value={target.sshUser} onChange={(event) => setTarget({ ...target, sshUser: event.target.value })} placeholder="留空使用本机 SSH 默认用户" autoCapitalize="none" spellCheck={false} /></label>
      <label>发布目录<input value={target.publishDirectory} onChange={(event) => setTarget({ ...target, publishDirectory: event.target.value })} placeholder="/www/wwwroot/mdoc/" autoCapitalize="none" spellCheck={false} aria-describedby="publication-directory-help" /></label>
      <p id="publication-directory-help" className="settings-section-copy">使用服务器上的绝对路径，至少三级目录，例如 /var/www/notes/。使用本机 SSH 密钥连接；不同知识库应使用不同目录，部署会删除该目录中已撤下的页面和附件。</p>
    </fieldset>
    <fieldset disabled={disabled}>
      <legend>站点</legend>
      <label>站点标题<input value={config.title} onChange={(event) => field("title", event.target.value)} /></label>
      <label>站点说明<input value={config.subtitle} onChange={(event) => field("subtitle", event.target.value)} /></label>
      <label>访问 URL<input type="url" value={config.baseURL} onChange={(event) => field("baseURL", event.target.value)} placeholder="https://mdoc.raoqu.cc/" /></label>
      <label className="publication-checkbox"><input type="checkbox" checked={config.includeTOC} onChange={(event) => field("includeTOC", event.target.checked)} />显示文章目录</label>
    </fieldset>
    <fieldset disabled={disabled}>
      <legend>Hugo 主题</legend>
      <p className="settings-section-copy">文件预览和发布共用 Hugo 主题。内置主题可直接使用；Stack 等模块主题首次使用前需安装。导航目录来自侧栏已固定文档的树结构，仅包含已发布且公开的节点。</p>
      <label>主题<select value={config.theme ?? ""} onChange={(event) => field("theme", event.target.value)}>{status.themes?.map((theme) => <option key={theme.id} value={theme.id}>{theme.label}</option>)}<option value="">自定义 Hugo Module</option></select></label>
      <label>目录树样式<input value={config.treeTheme ?? ""} onChange={(event) => field("treeTheme", event.target.value)} placeholder="跟随主题；可填 default 或 compact" /></label>
      {!config.theme ? <><label>主题模块<input value={config.themeModule} onChange={(event) => field("themeModule", event.target.value)} /></label><label>固定版本<input value={config.themeVersion} onChange={(event) => field("themeVersion", event.target.value)} /></label></> : selectedTheme?.module ? <p className="settings-section-copy">{selectedTheme.module} · {selectedTheme.version}</p> : null}
      {(!config.theme || selectedTheme?.module) ? <button type="button" onClick={() => void run("theme")}><Download size={15} />{working === "theme" ? "安装中…" : installed ? "检查主题安装" : "安装主题"}</button> : null}
      <span className="publication-theme-state">{installed ? "所选主题可用" : "所选主题尚未安装"}</span>
    </fieldset>
    <div className="publication-actions">
      <button type="button" disabled={disabled} onClick={() => void run("save")}><Save size={15} />保存配置</button>
      <button type="button" disabled={disabled || !installed} onClick={() => void run("build")}><Play size={15} />{working === "build" ? "生成中…" : "生成预览"}</button>
      <button className="primary-action" type="button" disabled={disabled || !installed || !target.serverIP.trim() || !target.publishDirectory.trim()} onClick={() => void run("deploy")}><Globe size={15} />{working === "deploy" ? "发布中…" : "发布到服务器"}</button>
      {built ? <a href="/site/" target="_blank" rel="noreferrer"><ExternalLink size={15} />查看本地站点</a> : null}
    </div>
    {pendingSave ? <p role="status">请等待笔记保存完成，并先处理保存冲突。</p> : null}
    <h2>待发布文档 · {status.documents.length}</h2>
    {status.documents.length === 0 ? <p className="empty-copy">还没有符合条件的文档。部署将生成空站点，并撤下服务器上的旧文档。</p> : <ul className="publication-documents">{status.documents.map((document) => <li key={document.id}>{document.title}</li>)}</ul>}
    {working ? <p role="status">正在{working === "theme" ? "安装主题" : working === "deploy" ? "构建并部署站点" : working === "build" ? "生成站点" : "保存配置"}…</p> : null}
    {log ? <pre className="publication-log" aria-label="发布日志">{log}</pre> : null}
  </section>;
}
