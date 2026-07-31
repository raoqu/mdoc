import assert from "node:assert/strict";
import test from "node:test";
import {
  aliasesFromFrontmatter,
  joinFrontmatter,
  privateFromFrontmatter,
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
