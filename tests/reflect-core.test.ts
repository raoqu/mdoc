import assert from "node:assert/strict";
import test from "node:test";
import { formatPublicationTarget, parsePublicationTarget } from "../app/reflect/publication-target";
import { deployPublication, PublicationSetupError, type PublicationStatus } from "../app/reflect/publication-api";
import { isEmptyNewNoteDraft, mergeNewNoteDrafts, notebooksWithSavableDrafts, UNTITLED_NOTE_TITLE } from "../app/reflect/note-drafts";
import { documentsInNotebook, type NotebookRecord } from "../app/reflect/types";
import { noteDeletionConfirmation } from "../app/reflect/note-deletion";
import {
  aliasesFromFrontmatter,
  joinFrontmatter,
  privateFromFrontmatter,
  publishFromFrontmatter,
  splitFrontmatter,
  upsertFrontmatter,
} from "../app/reflect/frontmatter";
import {
  appendTask,
  convertTaskToBullet,
  dailyNoteContent,
  editTask,
  isEmptyDailyNoteDraft,
  removeTask,
  renameWikiLinks,
  resolveConflictMarkers,
  rescheduleTask,
  taskBucket,
  tasksIn,
  toggleTask,
  wikiLinksIn,
} from "../app/reflect/markdown";
import {
  addMonths,
  buildMonthGrid,
  dailyDatesFromDocuments,
  monthLabel,
  monthOf,
  weekdayLabels,
} from "../app/reflect/month-grid";
import { normalizeLinkedImages } from "../app/reflect/reflect-editor";
import {
  chatModelOptions,
  resolveChatModel,
  type AiProviderConfig,
} from "../app/reflect/ai";
import { interleaveChatTools } from "../app/reflect/chat-transcript";
import {
  chooseClipboardInsertion,
  looksLikeMarkdown,
} from "../app/reflect/clipboard";

test("chat model options expose every curated model per configured key", () => {
  const providers: AiProviderConfig[] = [
    {
      id: "openai-key",
      provider: "openai",
      label: "OpenAI",
      model: "custom-model",
      keyHint: "••••1234",
      isDefault: true,
      createdAt: "2026-07-31T00:00:00Z",
    },
  ];
  const options = chatModelOptions(providers);
  assert.equal(options.some((option) => option.modelId === "gpt-5.6-sol"), true);
  assert.equal(options.some((option) => option.modelId === "custom-model"), true);
  assert.deepEqual(
    resolveChatModel(providers, {
      configId: "openai-key",
      modelId: "gpt-5.4-mini",
    }),
    { provider: providers[0], modelId: "gpt-5.4-mini" },
  );
  assert.equal(
    resolveChatModel(providers, {
      configId: "removed-key",
      modelId: "missing",
    })?.modelId,
    "custom-model",
  );
});

test("chat transcript preserves text around tool activity", () => {
  const search = { toolCallId: "search", textOffset: 6 };
  const read = { toolCallId: "read", textOffset: 6 };
  assert.deepEqual(
    interleaveChatTools("Before answer", [search, read]),
    [
      { kind: "text", text: "Before" },
      { kind: "tool", activity: search },
      { kind: "tool", activity: read },
      { kind: "text", text: " answer" },
    ],
  );
});

test("frontmatter remains byte-identical when only the body changes", () => {
  const source =
    "---\r\n# keep this comment\r\nprivate: true\r\naliases:\r\n  - Old title\r\n---\r\n# Note\r\n\r\nBody";
  const split = splitFrontmatter(source);
  assert.equal(split.raw?.includes("# keep this comment"), true);
  assert.equal(split.body, "# Note\r\n\r\nBody");
  assert.equal(
    joinFrontmatter(split.header, "# Note\r\n\r\nChanged"),
    source.replace("Body", "Changed"),
  );
});

test("sync conflict markers resolve to ours, theirs, or both", () => {
  const source = "before\n<<<<<<< this device\nours\n=======\ntheirs\n>>>>>>> other device\nafter\n";
  assert.equal(
    resolveConflictMarkers(source, "ours"),
    "before\nours\nafter\n",
  );
  assert.equal(
    resolveConflictMarkers(source, "theirs"),
    "before\ntheirs\nafter\n",
  );
  assert.equal(
    resolveConflictMarkers(source, "both"),
    "before\nours\ntheirs\nafter\n",
  );
});

test("task projection inherits daily dates and preserves list breadcrumbs", () => {
  const notebook = {
    id: "book",
    title: "Book",
    description: "",
    accent: "#000",
    folders: [],
  };
  const folder = { id: "daily", title: "Daily", open: true, docs: [], children: [] };
  const document = {
    id: "daily-2026-07-24",
    title: "Today",
    content: "- Launch\n  + [ ] Verify migration\n",
    updatedAt: "2026-07-24T00:00:00Z",
    revision: 0,
  };
  const tasks = tasksIn([{ notebook, folder, document }]);
  assert.equal(tasks[0]?.dueDate, "2026-07-24");
  assert.deepEqual(tasks[0]?.breadcrumbs, ["Launch"]);
});

test("task rescheduling is guarded against stale task text", () => {
  const source = "+ [ ] Ship [[2026-07-24]]";
  assert.equal(
    rescheduleTask(source, 0, "Ship [[2026-07-24]]", "2026-07-30"),
    "+ [ ] Ship [[2026-07-30]]",
  );
  assert.throws(
    () => rescheduleTask(source, 0, "A different task", "2026-07-30"),
    /发生变化/,
  );
});

test("task projection ignores fenced examples and trashed notes", () => {
  const notebook = {
    id: "book",
    title: "Book",
    description: "",
    accent: "#000",
    folders: [],
  };
  const folder = { id: "notes", title: "Notes", open: true, docs: [], children: [] };
  const visible = {
    id: "visible",
    title: "Visible",
    content: "```md\n+ [ ] Example only\n```\n\n+ [ ] Real task\n",
    updatedAt: "2026-07-24T00:00:00Z",
    revision: 0,
  };
  const trashed = {
    ...visible,
    id: "trashed",
    title: "Trashed",
    content: "+ [ ] Deleted task\n",
    trashed: true,
  };
  const tasks = tasksIn([
    { notebook, folder, document: visible },
    { notebook, folder, document: trashed },
  ]);
  assert.deepEqual(tasks.map((task) => task.content), ["Real task"]);
});

test("only an explicit past date makes a daily task overdue", () => {
  const notebook = {
    id: "book",
    title: "Book",
    description: "",
    accent: "#000",
    folders: [],
  };
  const folder = { id: "daily", title: "Daily", open: true, docs: [], children: [] };
  const document = {
    id: "daily-2026-07-20",
    title: "Past daily",
    content: "+ [ ] Carry forward\n+ [ ] Explicit [[2026-07-19]]\n",
    updatedAt: "2026-07-20T00:00:00Z",
    revision: 0,
  };
  const tasks = tasksIn([{ notebook, folder, document }]);
  assert.equal(taskBucket(tasks[0], "2026-07-31"), "current");
  assert.equal(taskBucket(tasks[1], "2026-07-31"), "overdue");
});

test("task row edits relocate safely and preserve markdown-backed operations", () => {
  const source = "# Plan\n\n+ [ ] Ship [[2026-08-01]]\n+ [x] Done\n";
  const shifted = `Intro\n${source}`;
  assert.equal(
    editTask(shifted, 2, "Ship [[2026-08-01]]", "Ship beta [[2026-08-02]]"),
    "Intro\n# Plan\n\n+ [ ] Ship beta [[2026-08-02]]\n+ [x] Done\n",
  );
  assert.equal(
    convertTaskToBullet(source, 4, "Done"),
    "# Plan\n\n+ [ ] Ship [[2026-08-01]]\n+ Done\n",
  );
  assert.equal(
    removeTask(source, 4, "Done"),
    "# Plan\n\n+ [ ] Ship [[2026-08-01]]\n",
  );
  assert.equal(appendTask("# Plan\n", "Review"), "# Plan\n+ [ ] Review\n");
  assert.throws(
    () =>
      toggleTask(
        "```md\n+ [ ] Example only\n```\n",
        1,
        "Example only",
      ),
    /发生变化/,
  );
});

test("frontmatter updates preserve unknown keys and remove false private flags", () => {
  const source = "---\ncustom: keep\nprivate: true\n---\n# Note\n";
  const updated = upsertFrontmatter(source, {
    private: undefined,
    aliases: ["Previous"],
  });
  assert.match(updated, /custom: keep/);
  assert.doesNotMatch(updated, /private:/);
  assert.deepEqual(aliasesFromFrontmatter(updated), ["Previous"]);
  assert.equal(privateFromFrontmatter(updated), false);
});

test("unterminated frontmatter is treated as ordinary markdown", () => {
  const source = "---\nprivate: true\n# Still content";
  const split = splitFrontmatter(source);
  assert.equal(split.header, "");
  assert.equal(split.body, source);
});

test("wiki-link rename preserves aliases and fragments but skips code fences", () => {
  const source =
    "[[Old]] [[old#Section]] [[OLD|Label]]\n\n```md\n[[Old]]\n```";
  const renamed = renameWikiLinks(source, "Old", "New");
  assert.equal(
    renamed,
    "[[New]] [[New#Section]] [[New|Label]]\n\n```md\n[[Old]]\n```",
  );
  assert.deepEqual(
    wikiLinksIn(renamed).map((link) => link.target),
    ["New", "New", "New", "Old"],
  );
});

test("round task toggles write back without changing the task body", () => {
  const source = "# Tasks\n\n+ [ ] Ship the migration [[2026-07-24]]\n";
  const checked = toggleTask(source, 2);
  assert.equal(
    checked,
    "# Tasks\n\n+ [x] Ship the migration [[2026-07-24]]\n",
  );
  assert.equal(toggleTask(checked, 2), source);
});

test("legacy linked images migrate to renderable image metadata", () => {
  assert.equal(
    normalizeLinkedImages(
      '[![cover](/uploads/cover.png)<!-- {"width":522,"height":294} -->](https://example.com)',
    ),
    '![cover](/uploads/cover.png)<!-- {"width":522,"height":294,"href":"https://example.com"} -->',
  );
});

test("smart paste prefers explicit or recognizable Markdown source", () => {
  assert.equal(looksLikeMarkdown("## Heading\n\n- first\n- second"), true);
  assert.equal(looksLikeMarkdown("A plain sentence with punctuation."), false);
  assert.deepEqual(
    chooseClipboardInsertion({
      plainText: "## Heading\n\n**important**",
      html: "<h2>Heading</h2><p><strong>important</strong></p>",
    }),
    { kind: "markdown", text: "## Heading\n\n**important**" },
  );
  assert.deepEqual(
    chooseClipboardInsertion({
      markdown: "- source",
      plainText: "source",
      html: "<ul><li>source</li></ul>",
    }),
    { kind: "markdown", text: "- source" },
  );
});

test("smart paste keeps rich formatting and falls back to plain text", () => {
  assert.deepEqual(
    chooseClipboardInsertion({
      plainText: "Heading\nimportant",
      html: "<h2>Heading</h2><p><strong>important</strong></p>",
    }),
    {
      kind: "rich-text",
      html: "<h2>Heading</h2><p><strong>important</strong></p>",
    },
  );
  assert.deepEqual(chooseClipboardInsertion({ plainText: "plain text" }), {
    kind: "plain-text",
    text: "plain text",
  });
  assert.equal(chooseClipboardInsertion({ plainText: "  " }), undefined);
});

test("month grid pads full Monday-first weeks and labels months", () => {
  assert.equal(monthOf("2026-06-09"), "2026-06");
  assert.equal(monthLabel("2026-06"), "2026年6月");
  assert.equal(addMonths("2026-12", 1), "2027-01");
  assert.equal(addMonths("2026-01", -1), "2025-12");
  assert.deepEqual(weekdayLabels(1), ["一", "二", "三", "四", "五", "六", "日"]);

  // June 2026 starts on Monday and ends on Tuesday.
  const june = buildMonthGrid("2026-06");
  assert.equal(june.start, "2026-06-01");
  assert.equal(june.end, "2026-07-05");
  assert.equal(june.weeks.length, 5);
  assert.equal(june.weeks[0]![0]!.date, "2026-06-01");
  assert.equal(june.weeks[0]![0]!.inMonth, true);

  // August 2026 starts on Saturday — five leading fill days.
  const august = buildMonthGrid("2026-08");
  assert.equal(august.start, "2026-07-27");
  assert.equal(
    august.weeks[0]!.slice(0, 5).every((cell) => !cell.inMonth),
    true,
  );
  assert.deepEqual(august.weeks[0]![5], {
    date: "2026-08-01",
    inMonth: true,
  });
});

test("daily dates are collected from non-trashed daily note ids", () => {
  const dates = dailyDatesFromDocuments([
    { id: "daily-2026-06-05" },
    { id: "daily-2026-06-09", trashed: true },
    { id: "note-hello" },
    { id: "daily-2026-06-18" },
  ]);
  assert.equal(dates.has("2026-06-05"), true);
  assert.equal(dates.has("2026-06-09"), false);
  assert.equal(dates.has("2026-06-18"), true);
  assert.equal(dates.has("2026-06-04"), false);
});

test("an unopened daily-note draft stays empty until content changes", () => {
  const date = "2026-07-29";
  assert.equal(
    isEmptyDailyNoteDraft(dailyNoteContent(date, false), date),
    true,
  );
  assert.equal(
    isEmptyDailyNoteDraft(dailyNoteContent(date, true), date),
    true,
  );
  assert.equal(
    isEmptyDailyNoteDraft(
      `${dailyNoteContent(date, false)}记录第一次编辑`,
      date,
    ),
    false,
  );
  assert.equal(
    isEmptyDailyNoteDraft("# 修改过的标题\n\n", date),
    false,
  );
});

test("new-note drafts distinguish empty editor scaffolding from title or body input", () => {
  for (const content of ["", "   \n", "# \n\n", "#\r\n\r\n- ", "---\nprivate: true\n---\n# \n"]) {
    assert.equal(isEmptyNewNoteDraft({ title: UNTITLED_NOTE_TITLE, content }), true, content);
  }
  for (const document of [
    { title: "只有标题", content: "" },
    { title: UNTITLED_NOTE_TITLE, content: "# 只有标题\n\n" },
    { title: UNTITLED_NOTE_TITLE, content: "# 未命名笔记\n\n" },
    { title: UNTITLED_NOTE_TITLE, content: "# \n\n只有正文" },
    { title: UNTITLED_NOTE_TITLE, content: "![图片](/uploads/image.png)" },
    { title: UNTITLED_NOTE_TITLE, content: "---\ntitle: 元数据标题\n---\n" },
    { title: UNTITLED_NOTE_TITLE, content: "---\ninvalid: [\n---\n" },
  ]) assert.equal(isEmptyNewNoteDraft(document), false, document.content);
});

test("structural saves omit only empty new drafts while preserving existing notes and nested content", () => {
  const document = (id: string, content: string) => ({ id, title: UNTITLED_NOTE_TITLE, content, updatedAt: "", revision: 0 });
  const records: NotebookRecord[] = [{
    id: "test", title: "Test", description: "", accent: "#5b4cf0",
    folders: [{ id: "notes", title: "笔记", open: true,
      docs: [document("saved-empty", ""), document("empty-new", "# \n\n"), document("body-new", "实际内容")],
      children: [{ id: "nested", title: "Nested", open: true,
        docs: [document("cleared-new", "# \n\n"), { ...document("title-new", ""), title: "有标题" }], children: [] }],
    }],
  }];
  const outgoing = notebooksWithSavableDrafts(records, new Set(["empty-new", "body-new", "cleared-new", "title-new"]));
  assert.deepEqual(documentsInNotebook(outgoing[0]).map(({ document }) => document.id), ["saved-empty", "body-new", "title-new"]);
  assert.equal(documentsInNotebook(records[0]).length, 5);
  assert.deepEqual(notebooksWithSavableDrafts(records, new Set()), records);
});

test("background refresh preserves local drafts and accepts server changes without reviving deleted saved notes", () => {
  const document = (id: string, title: string) => ({ id, title, content: "", updatedAt: "", revision: 0 });
  const notebook: NotebookRecord = {
    id: "test", title: "Test", description: "", accent: "#5b4cf0",
    folders: [{ id: "notes", title: "笔记", open: true,
      docs: [document("saved", "旧标题"), document("deleted", "已删除的笔记"), document("draft", UNTITLED_NOTE_TITLE)],
      children: [{ id: "draft-folder", title: "草稿目录", open: true, docs: [document("nested-draft", UNTITLED_NOTE_TITLE)], children: [] }],
    }],
  };
  const saved = [{ ...notebook, folders: [{ ...notebook.folders[0], docs: [document("saved", "服务器标题")], children: [] }] }];
  const merged = mergeNewNoteDrafts(saved, [notebook], new Set(["draft", "nested-draft"]));
  assert.deepEqual(documentsInNotebook(merged[0]).map(({ document }) => [document.id, document.title]), [
    ["saved", "服务器标题"], ["draft", UNTITLED_NOTE_TITLE], ["nested-draft", UNTITLED_NOTE_TITLE],
  ]);
  assert.deepEqual(mergeNewNoteDrafts(saved, merged, new Set(["draft", "nested-draft"])), merged);
  assert.deepEqual(mergeNewNoteDrafts(saved, [notebook], new Set()), saved);
  const savedDraft = [{ ...saved[0], folders: [{ ...saved[0].folders[0], docs: [...saved[0].folders[0].docs, document("draft", "已保存")], children: [] }] }];
  assert.equal(documentsInNotebook(mergeNewNoteDrafts(savedDraft, [notebook], new Set(["draft"]))[0]).length, 2);
});

test("single-note deletion confirms nonempty content while ignoring empty editor scaffolding", () => {
  for (const content of ["", " \n", "# \n\n", "#\r\n\r\n- "]) {
    assert.equal(noteDeletionConfirmation([{ title: "空白笔记", content }]), null);
  }
  for (const content of ["正文", "# 有标题", "![图片](/uploads/image.png)", "---\ntitle: 元数据标题\n---\n"]) {
    const message = noteDeletionConfirmation([{ title: "待删除笔记", content }]);
    assert.match(message ?? "", /待删除笔记/);
    assert.match(message ?? "", /废纸篓/);
  }
});

test("multi-note deletion always confirms, including a selection of empty notes", () => {
  assert.equal(noteDeletionConfirmation([]), null);
  assert.match(noteDeletionConfirmation([
    { title: "未命名笔记", content: "" }, { title: "未命名笔记", content: "" },
  ]) ?? "", /2 篇笔记/);
  assert.match(noteDeletionConfirmation([
    { title: "文档", content: "正文" }, { title: "未命名笔记", content: "" }, { title: "标题", content: "# 标题" },
  ]) ?? "", /3 篇笔记/);
});

test("publication server fields preserve legacy targets and optional SSH users", () => {
  for (const target of ["root@10.6.0.1:/www/wwwroot/mdoc/", "deploy@docs.example.com:/var/www/docs", "ssh-alias:/srv/sites/notes/", ""]) {
    assert.equal(formatPublicationTarget(parsePublicationTarget(target)), target);
  }
  assert.deepEqual(parsePublicationTarget("root@10.6.0.1:/www/wwwroot/mdoc/"), {
    serverIP: "10.6.0.1", sshUser: "root", publishDirectory: "/www/wwwroot/mdoc/",
  });
  assert.equal(formatPublicationTarget({ serverIP: " 192.0.2.10 ", sshUser: " deploy ", publishDirectory: " /var/www/team/ " }), "deploy@192.0.2.10:/var/www/team/");
  assert.equal(formatPublicationTarget({ serverIP: "", sshUser: "root", publishDirectory: "" }), "");
});

test("publication server fields reject incomplete and combined server addresses", () => {
  for (const target of [
    { serverIP: "192.0.2.10", sshUser: "", publishDirectory: "" },
    { serverIP: "", sshUser: "", publishDirectory: "/var/www/docs/" },
    { serverIP: "https://192.0.2.10", sshUser: "", publishDirectory: "/var/www/docs/" },
    { serverIP: "root@192.0.2.10", sshUser: "", publishDirectory: "/var/www/docs/" },
    { serverIP: "192.0.2.10:22", sshUser: "", publishDirectory: "/var/www/docs/" },
    { serverIP: "192.0.2.10", sshUser: "root@other", publishDirectory: "/var/www/docs/" },
  ]) assert.throws(() => formatPublicationTarget(target));
});

test("publication marker requires a YAML boolean and supports revocation", () => {
  assert.equal(publishFromFrontmatter("# Note\npublish: true"), false);
  assert.equal(publishFromFrontmatter("---\npublish: 'true'\n---\nNote"), false);
  assert.equal(publishFromFrontmatter("---\npublish: true\ninvalid: [\n---\nNote"), false);
  const selected = upsertFrontmatter("# Note\n", { publish: true });
  assert.equal(publishFromFrontmatter(selected), true);
  assert.equal(publishFromFrontmatter(upsertFrontmatter(selected, { publish: false })), false);
});

function publicationStatusFixture(): PublicationStatus {
  return {
    config: {
      title: "Test", baseURL: "https://docs.example.test/", subtitle: "",
      themeModule: "example.test/theme", themeVersion: "v1.0.0", includeTOC: true,
      deployTarget: "deploy@192.0.2.10:/var/www/notes/",
    },
    directory: "/workspace/publishing/first", documents: [], themeInstalled: true, previewURL: "/site/",
  };
}

test("quick publication uses saved settings and pins the deployment to their knowledge base", async (t) => {
  const status = publicationStatusFixture();
  const requests: { url: string; init?: RequestInit }[] = [];
  t.mock.method(globalThis, "fetch", async (url: string, init?: RequestInit) => {
    requests.push({ url, init });
    return Response.json(url === "/api/publication" ? status : { documents: 3, log: "Published" });
  });
  const deployment = await deployPublication();
  assert.equal(deployment.result.documents, 3);
  assert.deepEqual(requests.map((request) => [request.url, request.init?.method ?? "GET"]), [
    ["/api/publication", "GET"], ["/api/publication/deploy", "POST"],
  ]);
  assert.equal(new Headers(requests[1].init?.headers).get("X-Mdoc-Publication-Directory"), status.directory);
});

test("settings publication saves the edited target before deployment", async (t) => {
  const status = publicationStatusFixture();
  status.config.deployTarget = "deploy@192.0.2.20:/var/www/other/";
  const requests: RequestInit[] = [];
  t.mock.method(globalThis, "fetch", async (url: string, init: RequestInit) => {
    requests.push(init);
    return Response.json(url === "/api/publication" ? status : { documents: 1, log: "Published" });
  });
  await deployPublication({ directory: status.directory, config: status.config });
  assert.equal(requests[0].method, "PUT");
  assert.equal(JSON.parse(String(requests[0].body)).deployTarget, status.config.deployTarget);
  assert.equal(new Headers(requests[0].headers).get("X-Mdoc-Publication-Directory"), status.directory);
  assert.equal(requests[1].method, "POST");
});

test("missing publication prerequisites prevent deployment", async (t) => {
  const status = publicationStatusFixture();
  const request = t.mock.method(globalThis, "fetch", async () => Response.json(status));
  status.config.deployTarget = "";
  await assert.rejects(deployPublication(), PublicationSetupError);
  status.config.deployTarget = "deploy@192.0.2.10:/var/www/notes/";
  status.themeInstalled = false;
  await assert.rejects(deployPublication(), PublicationSetupError);
  assert.equal(request.mock.calls.length, 2);
});

test("publication failures propagate the server reason for the completion notice", async (t) => {
  const status = publicationStatusFixture();
  t.mock.method(globalThis, "fetch", async (url: string) => url === "/api/publication"
    ? Response.json(status)
    : new Response("SSH connection failed\n", { status: 500 }));
  await assert.rejects(deployPublication(), /SSH connection failed/);
});
