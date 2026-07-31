"use client";

import {
  Fragment,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
  type MouseEvent,
} from "react";
import {
  Archive,
  CalendarClock,
  Circle,
  CircleCheck,
  Clock3,
  FileText,
  List,
  ListFilter,
  Pin,
  Plus,
  Search,
  Trash2,
  X,
} from "lucide-react";
import {
  localDateKey,
  taskBucket,
  tasksIn,
  type MarkdownTask,
  type TaskBucket,
} from "./markdown";
import type { DocumentLocation } from "./types";

interface TaskFilters {
  pinned: boolean;
  current: boolean;
  overdue: boolean;
  upcoming: boolean;
  other: boolean;
  archived: boolean;
}

const DEFAULT_FILTERS: TaskFilters = {
  pinned: true,
  current: true,
  overdue: true,
  upcoming: true,
  other: true,
  archived: false,
};

interface TaskGroup {
  key: string;
  kind: TaskBucket;
  label: string;
  documentId: string | null;
  pinned: boolean;
  tasks: MarkdownTask[];
}

interface AddTaskTarget {
  date?: string;
  documentId?: string;
  label: string;
}

interface TasksScreenProps {
  documents: readonly DocumentLocation[];
  onOpenDocument: (documentId: string) => void;
  onToggleTasks: (tasks: readonly MarkdownTask[]) => void;
  onScheduleTasks: (
    tasks: readonly MarkdownTask[],
    date: string | null,
  ) => void;
  onEditTask: (task: MarkdownTask, content: string) => void;
  onDeleteTasks: (tasks: readonly MarkdownTask[]) => void;
  onConvertTasks: (tasks: readonly MarkdownTask[]) => void;
  onAddTask: (target: AddTaskTarget, content: string) => void;
}

function visibleTaskText(content: string): string {
  return (
    content
      .replace(/\s*\[\[\d{4}-\d{2}-\d{2}\]\]\s*/, " ")
      .replace(/\s+/g, " ")
      .trim() || "空任务"
  );
}

function compareDatedTasks(left: MarkdownTask, right: MarkdownTask): number {
  const leftDate = left.dueDate ?? "";
  const rightDate = right.dueDate ?? "";
  if (leftDate !== rightDate) return leftDate.localeCompare(rightDate);
  if (left.documentTitle !== right.documentTitle) {
    return left.documentTitle.localeCompare(right.documentTitle, "zh-CN");
  }
  return left.line - right.line;
}

function groupTasks(tasks: readonly MarkdownTask[], today: string): TaskGroup[] {
  const dated = new Map<TaskBucket, MarkdownTask[]>([
    ["current", []],
    ["overdue", []],
    ["upcoming", []],
  ]);
  const byDocument = new Map<string, MarkdownTask[]>();

  tasks.forEach((task) => {
    const bucket = taskBucket(task, today);
    if (bucket === "note") {
      const group = byDocument.get(task.documentId);
      if (group) group.push(task);
      else byDocument.set(task.documentId, [task]);
      return;
    }
    dated.get(bucket)!.push(task);
  });

  const groups: TaskGroup[] = [];
  const labels: Record<Exclude<TaskBucket, "note">, string> = {
    current: "当前",
    overdue: "已逾期",
    upcoming: "即将到来",
  };
  (["current", "overdue", "upcoming"] as const).forEach((kind) => {
    const rows = dated.get(kind)!;
    if (rows.length > 0) {
      groups.push({
        key: kind,
        kind,
        label: labels[kind],
        documentId: null,
        pinned: false,
        tasks: rows.sort(compareDatedTasks),
      });
    }
  });

  const noteGroups = [...byDocument.values()]
    .map((rows): TaskGroup => ({
      key: `note:${rows[0].documentId}`,
      kind: "note",
      label: rows[0].documentTitle,
      documentId: rows[0].documentId,
      pinned: rows[0].documentPinned,
      tasks: rows.sort((left, right) => left.line - right.line),
    }))
    .sort((left, right) => {
      if (left.pinned !== right.pinned) return left.pinned ? -1 : 1;
      const leftUpdated = left.tasks[0]?.documentUpdatedAt ?? "";
      const rightUpdated = right.tasks[0]?.documentUpdatedAt ?? "";
      if (leftUpdated !== rightUpdated) return rightUpdated.localeCompare(leftUpdated);
      return left.label.localeCompare(right.label, "zh-CN");
    });

  return [...groups, ...noteGroups];
}

function sameBreadcrumbs(
  left: readonly string[] | undefined,
  right: readonly string[],
): boolean {
  return (
    left !== undefined &&
    left.length === right.length &&
    left.every((part, index) => part === right[index])
  );
}

function GroupIcon({ group }: { group: TaskGroup }) {
  if (group.kind === "current") return <Clock3 size={15} />;
  if (group.kind === "overdue") return <CalendarClock size={15} />;
  if (group.kind === "upcoming") return <CalendarClock size={15} />;
  if (group.pinned) return <Pin size={14} />;
  return <FileText size={14} />;
}

export function TasksScreen({
  documents,
  onOpenDocument,
  onToggleTasks,
  onScheduleTasks,
  onEditTask,
  onDeleteTasks,
  onConvertTasks,
  onAddTask,
}: TasksScreenProps) {
  const today = localDateKey();
  const allTasks = useMemo(() => tasksIn(documents), [documents]);
  const [query, setQuery] = useState("");
  const [filters, setFilters] = useState<TaskFilters>(DEFAULT_FILTERS);
  const [recentlyCompleted, setRecentlyCompleted] = useState<Set<string>>(
    () => new Set(),
  );
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [selectionAnchor, setSelectionAnchor] = useState<string | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [composerTarget, setComposerTarget] = useState<AddTaskTarget | null>(null);
  const [composerText, setComposerText] = useState("");
  const rootRef = useRef<HTMLDivElement>(null);
  const composerRef = useRef<HTMLInputElement>(null);
  const filtersHydratedRef = useRef(false);

  useEffect(() => {
    let stored: TaskFilters | null = null;
    try {
      const raw = window.sessionStorage.getItem("mdocman.reflect.task-filters");
      if (raw) {
        stored = {
          ...DEFAULT_FILTERS,
          ...(JSON.parse(raw) as Partial<TaskFilters>),
        };
      }
    } catch {
      // Session storage is optional; defaults remain usable.
    }
    queueMicrotask(() => {
      filtersHydratedRef.current = true;
      if (stored) setFilters(stored);
    });
  }, []);

  useEffect(() => {
    if (!filtersHydratedRef.current) return;
    window.sessionStorage.setItem(
      "mdocman.reflect.task-filters",
      JSON.stringify(filters),
    );
  }, [filters]);

  useEffect(() => {
    if (composerTarget) composerRef.current?.focus();
  }, [composerTarget]);

  const needle = query.trim().toLocaleLowerCase();
  const visibleTasks = allTasks.filter((task) => {
    if (task.checked && !filters.archived && !recentlyCompleted.has(task.id)) {
      return false;
    }
    const bucket = taskBucket(task, today);
    if (bucket === "current" && !filters.current) return false;
    if (bucket === "overdue" && !filters.overdue) return false;
    if (bucket === "upcoming" && !filters.upcoming) return false;
    if (
      bucket === "note" &&
      !(task.documentPinned ? filters.pinned : filters.other)
    ) {
      return false;
    }
    if (!needle) return true;
    return [task.content, task.documentTitle, ...task.breadcrumbs].some(
      (value) => value.toLocaleLowerCase().includes(needle),
    );
  });
  const groups = groupTasks(visibleTasks, today);
  const orderedTasks = groups.flatMap((group) => group.tasks);
  const tasksById = new Map(orderedTasks.map((task) => [task.id, task]));
  const selectedTasks = [...selected]
    .map((id) => tasksById.get(id))
    .filter((task): task is MarkdownTask => Boolean(task));

  const openComposer = (target: AddTaskTarget) => {
    setComposerTarget(target);
    setComposerText("");
  };

  const submitComposer = (event: FormEvent) => {
    event.preventDefault();
    if (!composerTarget || !composerText.trim()) return;
    onAddTask(composerTarget, composerText);
    setComposerTarget(null);
    setComposerText("");
  };

  const startEditing = (task: MarkdownTask) => {
    setSelected(new Set([task.id]));
    setSelectionAnchor(task.id);
    setEditingId(task.id);
    setDraft(task.content);
  };

  const commitEditing = () => {
    if (!editingId) return;
    const task = allTasks.find((candidate) => candidate.id === editingId);
    const content = draft.trim();
    setEditingId(null);
    if (!task || content === task.content) return;
    if (content) onEditTask(task, content);
    else onDeleteTasks([task]);
  };

  const selectTask = (task: MarkdownTask, event: MouseEvent) => {
    const orderedIds = orderedTasks.map((candidate) => candidate.id);
    if (event.shiftKey && selectionAnchor) {
      const anchorIndex = orderedIds.indexOf(selectionAnchor);
      const targetIndex = orderedIds.indexOf(task.id);
      if (anchorIndex >= 0 && targetIndex >= 0) {
        const [from, to] =
          anchorIndex < targetIndex
            ? [anchorIndex, targetIndex]
            : [targetIndex, anchorIndex];
        setSelected(new Set(orderedIds.slice(from, to + 1)));
        setEditingId(null);
        return;
      }
    }
    if (event.metaKey || event.ctrlKey) {
      setSelected((current) => {
        const next = new Set(current);
        if (next.has(task.id)) next.delete(task.id);
        else next.add(task.id);
        return next;
      });
      setSelectionAnchor(task.id);
      setEditingId(null);
      return;
    }
    startEditing(task);
  };

  const toggleTasks = (tasks: readonly MarkdownTask[], checked: boolean) => {
    const targets = tasks.filter((task) => task.checked !== checked);
    if (targets.length === 0) return;
    setRecentlyCompleted((current) => {
      const next = new Set(current);
      targets.forEach((task) => {
        if (checked) next.add(task.id);
        else next.delete(task.id);
      });
      return next;
    });
    onToggleTasks(targets);
  };

  const toggleFromCheckbox = (task: MarkdownTask) => {
    const selectionContainsTask = selected.has(task.id);
    const targets =
      selectionContainsTask && selectedTasks.length > 1 ? selectedTasks : [task];
    toggleTasks(targets, !task.checked);
  };

  const scheduleSelected = (date: string | null) => {
    onScheduleTasks(selectedTasks, date);
    setSelected(new Set());
    setEditingId(null);
  };

  const convertSelected = () => {
    const targets = [...selectedTasks]
      .sort((left, right) =>
        left.documentId === right.documentId
          ? right.line - left.line
          : left.documentId.localeCompare(right.documentId),
      );
    onConvertTasks(targets);
    setSelected(new Set());
    setEditingId(null);
  };

  const deleteSelected = () => {
    const targets = [...selectedTasks]
      .sort((left, right) =>
        left.documentId === right.documentId
          ? right.line - left.line
          : left.documentId.localeCompare(right.documentId),
      );
    onDeleteTasks(targets);
    setSelected(new Set());
    setEditingId(null);
  };

  const handleKeyboard = (event: KeyboardEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement;
    if (target.closest("input, textarea, button, summary")) return;
    const modifier = event.metaKey || event.ctrlKey;
    if (modifier && event.key.toLocaleLowerCase() === "a") {
      event.preventDefault();
      setSelected(new Set(orderedTasks.map((task) => task.id)));
      setEditingId(null);
      return;
    }
    if (event.key === "Escape") {
      setSelected(new Set());
      setEditingId(null);
      return;
    }
    if (modifier && event.key === "Enter" && selectedTasks.length > 0) {
      event.preventDefault();
      toggleTasks(selectedTasks, true);
      return;
    }
    if (
      modifier &&
      event.shiftKey &&
      event.key.toLocaleLowerCase() === "k" &&
      selectedTasks.length > 0
    ) {
      event.preventDefault();
      convertSelected();
      return;
    }
    if (
      event.key !== "ArrowDown" &&
      event.key !== "ArrowUp" &&
      event.key !== " "
    ) {
      return;
    }
    event.preventDefault();
    if (event.key === " " && selectedTasks.length > 0) {
      toggleTasks(selectedTasks, !selectedTasks[0].checked);
      return;
    }
    if (orderedTasks.length === 0) return;
    const selectedIndex = orderedTasks.findIndex((task) => selected.has(task.id));
    const nextIndex =
      event.key === "ArrowDown"
        ? Math.min(orderedTasks.length - 1, selectedIndex + 1)
        : Math.max(0, selectedIndex < 0 ? orderedTasks.length - 1 : selectedIndex - 1);
    const task = orderedTasks[nextIndex];
    setSelected(new Set([task.id]));
    setSelectionAnchor(task.id);
    setEditingId(null);
    rootRef.current
      ?.querySelector<HTMLElement>(`[data-task-id="${CSS.escape(task.id)}"]`)
      ?.scrollIntoView({ block: "nearest" });
  };

  const toggleFilter = (key: keyof TaskFilters) => {
    setFilters((current) => ({ ...current, [key]: !current[key] }));
    setSelected(new Set());
    setEditingId(null);
  };

  return (
    <section
      className="tasks-view"
      ref={rootRef}
      tabIndex={-1}
      onKeyDown={handleKeyboard}
      aria-label="任务"
    >
      <header className="tasks-toolbar">
        <label className="tasks-search">
          <Search size={15} />
          <input
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setSelected(new Set());
              setEditingId(null);
            }}
            placeholder="搜索任务…"
            aria-label="搜索任务"
          />
        </label>

        {selectedTasks.length > 0 ? (
          <div className="tasks-selection-actions">
            <span className="task-selection-count">{selectedTasks.length}</span>
            <label title="安排日期">
              <CalendarClock size={15} />
              <input
                type="date"
                aria-label={`安排 ${selectedTasks.length} 个任务`}
                onChange={(event) =>
                  scheduleSelected(event.target.value || null)
                }
              />
            </label>
            <button
              type="button"
              onClick={() => scheduleSelected(null)}
              title="清除日期"
              aria-label="清除日期"
            >
              <X size={15} />
            </button>
            <button type="button" onClick={convertSelected} title="转为项目符号">
              <List size={15} />
              <span>转为项目符号</span>
            </button>
            <button type="button" onClick={deleteSelected} title="删除任务">
              <Trash2 size={15} />
            </button>
          </div>
        ) : null}

        {recentlyCompleted.size > 0 ? (
          <button
            type="button"
            className="task-toolbar-button"
            onClick={() => setRecentlyCompleted(new Set())}
          >
            <Archive size={15} />
            归档
            <span>{recentlyCompleted.size}</span>
          </button>
        ) : null}

        <details className="task-filters">
          <summary>
            <ListFilter size={15} />
            任务筛选
          </summary>
          <div className="task-filter-menu">
            <strong>任务</strong>
            {(
              [
                ["pinned", "已固定笔记"],
                ["current", "当前任务"],
                ["overdue", "逾期任务"],
                ["upcoming", "未来任务"],
                ["other", "其他任务"],
              ] as const
            ).map(([key, label]) => (
              <label key={key}>
                <input
                  type="checkbox"
                  checked={filters[key]}
                  onChange={() => toggleFilter(key)}
                />
                <span>{label}</span>
              </label>
            ))}
            <hr />
            <label>
              <input
                type="checkbox"
                checked={filters.archived}
                onChange={() => toggleFilter("archived")}
              />
              <span>显示已归档任务</span>
            </label>
          </div>
        </details>

        <button
          type="button"
          className="task-add-primary"
          onClick={() =>
            openComposer({ date: today, label: "今天的每日笔记" })
          }
        >
          <Plus size={15} />
          添加任务
        </button>
      </header>

      {composerTarget ? (
        <form className="task-composer" onSubmit={submitComposer}>
          <Circle size={18} />
          <input
            ref={composerRef}
            value={composerText}
            onChange={(event) => setComposerText(event.target.value)}
            placeholder={`添加到${composerTarget.label}`}
            aria-label={`添加任务到${composerTarget.label}`}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                setComposerTarget(null);
                setComposerText("");
              }
            }}
          />
          <button type="submit">添加</button>
          <button
            type="button"
            aria-label="取消添加任务"
            onClick={() => setComposerTarget(null)}
          >
            <X size={15} />
          </button>
        </form>
      ) : null}

      <div className="tasks-scroll">
        {groups.length === 0 ? (
          <div className="tasks-empty">
            <CircleCheck size={30} />
            <strong>{needle ? "没有匹配的任务" : "没有可显示的任务"}</strong>
            <span>
              {needle
                ? "换个关键词，或检查任务筛选。"
                : "在笔记中输入 + [ ]，任务会自动汇总到这里。"}
            </span>
          </div>
        ) : (
          <div className="task-groups">
            {groups.map((group) => {
              let previousBreadcrumbs: readonly string[] | undefined;
              const addTarget =
                group.kind === "current"
                  ? { date: today, label: "今天的每日笔记" }
                  : group.kind === "note" && group.documentId
                    ? { documentId: group.documentId, label: group.label }
                    : null;
              return (
                <section className={`task-group task-group-${group.kind}`} key={group.key}>
                  <header>
                    <h2>
                      <GroupIcon group={group} />
                      {group.documentId ? (
                        <button
                          type="button"
                          onClick={() => onOpenDocument(group.documentId!)}
                        >
                          {group.label}
                        </button>
                      ) : (
                        <span>{group.label}</span>
                      )}
                    </h2>
                    <span className="task-group-count">{group.tasks.length}</span>
                    {addTarget ? (
                      <button
                        type="button"
                        className="task-group-add"
                        onClick={() => openComposer(addTarget)}
                      >
                        <Plus size={13} />
                        添加
                      </button>
                    ) : null}
                  </header>
                  <ul>
                    {group.tasks.map((task) => {
                      const showBreadcrumbs = !sameBreadcrumbs(
                        previousBreadcrumbs,
                        task.breadcrumbs,
                      );
                      previousBreadcrumbs = task.breadcrumbs;
                      const isSelected = selected.has(task.id);
                      const isEditing = editingId === task.id;
                      return (
                        <Fragment key={task.id}>
                          {showBreadcrumbs && task.breadcrumbs.length > 0 ? (
                            <li className="task-context">
                              {task.breadcrumbs.join(" → ")}
                            </li>
                          ) : null}
                          <li
                            data-task-id={task.id}
                            className={`task-row ${task.checked ? "completed" : ""} ${isSelected ? "selected" : ""}`}
                          >
                            <button
                              type="button"
                              className="task-checkbox"
                              onClick={() => toggleFromCheckbox(task)}
                              aria-label={
                                task.checked
                                  ? `重新打开：${visibleTaskText(task.content)}`
                                  : `完成：${visibleTaskText(task.content)}`
                              }
                            >
                              {task.checked ? (
                                <CircleCheck size={18} />
                              ) : (
                                <Circle size={18} />
                              )}
                            </button>
                            {isEditing ? (
                              <input
                                className="task-inline-editor"
                                autoFocus
                                value={draft}
                                onChange={(event) => setDraft(event.target.value)}
                                onBlur={commitEditing}
                                onKeyDown={(event) => {
                                  if (event.key === "Enter") {
                                    event.preventDefault();
                                    commitEditing();
                                  } else if (event.key === "Escape") {
                                    event.preventDefault();
                                    setEditingId(null);
                                  }
                                }}
                                aria-label={`编辑任务：${visibleTaskText(task.content)}`}
                              />
                            ) : (
                              <button
                                type="button"
                                className="task-content"
                                aria-pressed={isSelected}
                                onClick={(event) => selectTask(task, event)}
                              >
                                <span>{visibleTaskText(task.content)}</span>
                                {task.explicitDate ? (
                                  <time dateTime={task.explicitDate}>
                                    <CalendarClock size={12} />
                                    {task.explicitDate}
                                  </time>
                                ) : null}
                              </button>
                            )}
                            {group.kind !== "note" ? (
                              <button
                                type="button"
                                className="task-source"
                                onClick={() => onOpenDocument(task.documentId)}
                              >
                                {task.dailyDate ?? task.documentTitle}
                              </button>
                            ) : null}
                          </li>
                        </Fragment>
                      );
                    })}
                  </ul>
                </section>
              );
            })}
          </div>
        )}
      </div>
    </section>
  );
}
