import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

async function render() {
  const workerUrl = new URL("../dist/server/index.js", import.meta.url);
  workerUrl.searchParams.set("test", `${process.pid}-${Date.now()}`);
  const { default: worker } = await import(workerUrl.href);

  return worker.fetch(
    new Request("http://localhost/", {
      headers: { accept: "text/html" },
    }),
    {
      ASSETS: {
        fetch: async () => new Response("Not found", { status: 404 }),
      },
    },
    {
      waitUntil() {},
      passThroughOnException() {},
    },
  );
}

test("server-renders the Reflect workspace shell", async () => {
  const response = await render();
  assert.equal(response.status, 200);
  assert.match(response.headers.get("content-type") ?? "", /^text\/html\b/i);

  const html = await response.text();
  assert.match(html, /<title>墨笺 · 关联式 Markdown 笔记<\/title>/);
  assert.match(html, /class="reflect-app/);
  assert.match(html, /每日笔记/);
  assert.match(html, /全部笔记/);
  assert.match(html, /任务/);
  assert.match(html, /搜索/);
  assert.doesNotMatch(html, /codex-preview|Your site is taking shape|Codex is working/);
});

test("ships the migrated editor and graph capabilities", async () => {
  const [workspace, sidebar, editor, markdown, tasks, packageJson] = await Promise.all([
    readFile(new URL("../app/reflect/workspace.tsx", import.meta.url), "utf8"),
    readFile(new URL("../app/reflect/workspace-sidebar.tsx", import.meta.url), "utf8"),
    readFile(new URL("../app/reflect/reflect-editor.tsx", import.meta.url), "utf8"),
    readFile(new URL("../app/reflect/markdown.ts", import.meta.url), "utf8"),
    readFile(new URL("../app/reflect/tasks-screen.tsx", import.meta.url), "utf8"),
    readFile(new URL("../package.json", import.meta.url), "utf8"),
  ]);

  assert.match(packageJson, /"@meowdown\/react": "0\.50\.0"/);
  assert.match(editor, /<MeowdownEditor/);
  assert.match(editor, /onWikilinkSearch/);
  assert.match(editor, /onTagSearch/);
  assert.match(editor, /onSlashMenuSearch/);
  assert.match(editor, /onFilePaste/);
  assert.match(workspace, /kind: "daily"/);
  assert.match(workspace, /isEmptyDailyNoteDraft/);
  assert.doesNotMatch(workspace, /ensureDaily/);
  assert.match(workspace, /backlinksFor/);
  assert.match(workspace, /<TasksScreen/);
  assert.match(tasks, /tasksIn/);
  assert.match(tasks, /任务筛选/);
  assert.match(workspace, /CommandPalette/);
  assert.match(workspace, /\/api\/semantic\/similar/);
  assert.match(workspace, /相似笔记/);
  assert.match(markdown, /WIKI_LINK_PATTERN/);
  assert.match(markdown, /TASK_PATTERN/);
  assert.match(sidebar, /className="knowledge-base-footer"/);
  assert.match(sidebar, /知识库颜色/);
  assert.match(sidebar, /在 Finder 中显示知识库/);
  assert.match(sidebar, /新建知识库…/);
  assert.doesNotMatch(sidebar, /数据库|SQLite|database-switcher/);
});
