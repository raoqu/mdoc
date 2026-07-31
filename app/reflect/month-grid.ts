/**
 * Pure month-grid math for the daily sidebar calendar.
 * Months are `YYYY-MM`; days are ISO `YYYY-MM-DD` (local calendar).
 * Weeks start on Monday by default (ISO 8601).
 */

export interface MonthGridCell {
  date: string;
  inMonth: boolean;
}

export interface MonthGrid {
  month: string;
  weeks: MonthGridCell[][];
  start: string;
  end: string;
}

function parseMonthParts(month: string): { year: number; monthIndex: number } {
  const match = /^(\d{4})-(\d{2})$/.exec(month);
  if (!match) {
    throw new Error(`expected a YYYY-MM month, got: ${month}`);
  }
  const year = Number(match[1]);
  const monthIndex = Number(match[2]) - 1;
  if (monthIndex < 0 || monthIndex > 11) {
    throw new Error(`expected a YYYY-MM month, got: ${month}`);
  }
  return { year, monthIndex };
}

function toIsoDate(year: number, monthIndex: number, day: number): string {
  const value = new Date(year, monthIndex, day);
  const y = value.getFullYear();
  const m = String(value.getMonth() + 1).padStart(2, "0");
  const d = String(value.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

function addDaysIso(date: string, days: number): string {
  const value = new Date(`${date}T12:00:00`);
  value.setDate(value.getDate() + days);
  const y = value.getFullYear();
  const m = String(value.getMonth() + 1).padStart(2, "0");
  const d = String(value.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

/** The `YYYY-MM` month containing the ISO `date`. */
export function monthOf(date: string): string {
  return date.slice(0, 7);
}

/** Human label for a `YYYY-MM` month, e.g. `2026年6月`. */
export function monthLabel(month: string): string {
  const { year, monthIndex } = parseMonthParts(month);
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "long",
  }).format(new Date(year, monthIndex, 1));
}

/** The `YYYY-MM` month `delta` months after `month` (negative for before). */
export function addMonths(month: string, delta: number): string {
  const { year, monthIndex } = parseMonthParts(month);
  const value = new Date(year, monthIndex + delta, 1);
  const y = value.getFullYear();
  const m = String(value.getMonth() + 1).padStart(2, "0");
  return `${y}-${m}`;
}

/**
 * Two-character weekday labels for the grid header.
 * @param weekStartsOn - 1 for Monday (default); 0 for Sunday.
 */
export function weekdayLabels(weekStartsOn: 0 | 1 = 1): string[] {
  // zh-CN short weekdays starting from Sunday: 日 一 二 …
  const sundayFirst = ["日", "一", "二", "三", "四", "五", "六"];
  if (weekStartsOn === 0) {
    return sundayFirst;
  }
  return [...sundayFirst.slice(1), sundayFirst[0]!];
}

/**
 * Build the full-week grid for a `YYYY-MM` month.
 * @param weekStartsOn - 1 for Monday (default); 0 for Sunday.
 */
export function buildMonthGrid(
  month: string,
  weekStartsOn: 0 | 1 = 1,
): MonthGrid {
  const { year, monthIndex } = parseMonthParts(month);
  const monthStart = new Date(year, monthIndex, 1);
  const monthEnd = new Date(year, monthIndex + 1, 0);

  const startDow = monthStart.getDay(); // 0=Sun … 6=Sat
  const endDow = monthEnd.getDay();
  const leading =
    weekStartsOn === 1
      ? (startDow + 6) % 7 // Monday-first: Sun→6, Mon→0
      : startDow;
  const trailing =
    weekStartsOn === 1
      ? (7 - ((endDow + 6) % 7) - 1) % 7
      : (6 - endDow + 7) % 7;

  const gridStart = toIsoDate(year, monthIndex, 1 - leading);
  const gridEnd = toIsoDate(year, monthIndex + 1, trailing);

  const weeks: MonthGridCell[][] = [];
  let cursor = gridStart;
  while (cursor <= gridEnd) {
    const week: MonthGridCell[] = [];
    for (let day = 0; day < 7; day += 1) {
      week.push({ date: cursor, inMonth: monthOf(cursor) === month });
      cursor = addDaysIso(cursor, 1);
    }
    weeks.push(week);
  }
  return { month, weeks, start: gridStart, end: gridEnd };
}

/** Collect ISO dates that already have a non-trashed daily note. */
export function dailyDatesFromDocuments(
  documents: readonly { id: string; trashed?: boolean }[],
): Set<string> {
  const dates = new Set<string>();
  for (const document of documents) {
    if (document.trashed) continue;
    const match = /^daily-(\d{4}-\d{2}-\d{2})$/.exec(document.id);
    if (match) {
      dates.add(match[1]!);
    }
  }
  return dates;
}
