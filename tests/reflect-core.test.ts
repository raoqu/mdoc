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
  renameWikiLinks,
  resolveConflictMarkers,
  rescheduleTask,
  tasksIn,
  toggleTask,
  wikiLinksIn,
} from "../app/reflect/markdown";
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
