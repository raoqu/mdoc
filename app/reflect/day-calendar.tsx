"use client";

import { useMemo, useState, type ReactElement } from "react";
import { CalendarDays, ChevronLeft, ChevronRight } from "lucide-react";
import { formatDailyTitle, localDateKey } from "./markdown";
import {
  addMonths,
  buildMonthGrid,
  monthLabel,
  monthOf,
  weekdayLabels,
} from "./month-grid";

export interface DayCalendarProps {
  /** Currently selected daily date (`YYYY-MM-DD`). */
  selectedDate: string;
  /** ISO dates that already have a daily note. */
  notedDates: ReadonlySet<string>;
  /** Navigate to a daily note for the given date. */
  onSelectDate: (date: string) => void;
  /** Jump to today's daily note. */
  onToday: () => void;
}

/**
 * Compact month calendar for the daily context sidebar.
 * Matches Reflect Open: selected day on an inverse square, today on a soft
 * square, and days with existing daily notes show a dot while the pointer
 * hovers the calendar.
 */
export function DayCalendar({
  selectedDate,
  notedDates,
  onSelectDate,
  onToday,
}: DayCalendarProps): ReactElement {
  const today = localDateKey();
  const [month, setMonth] = useState(() => monthOf(selectedDate));

  const grid = useMemo(() => buildMonthGrid(month, 1), [month]);
  const weekdays = useMemo(() => weekdayLabels(1), []);

  return (
    <div aria-label="日历" className="day-calendar">
      <header className="day-calendar-header">
        <div className="day-calendar-title">{monthLabel(month)}</div>
        <nav className="day-calendar-nav" aria-label="月份导航">
          <button
            type="button"
            aria-label="上个月"
            onClick={() => setMonth(addMonths(month, -1))}
          >
            <ChevronLeft size={16} />
          </button>
          <button
            type="button"
            aria-label="跳到今天"
            title="跳到今天"
            onClick={onToday}
          >
            <CalendarDays size={15} />
          </button>
          <button
            type="button"
            aria-label="下个月"
            onClick={() => setMonth(addMonths(month, 1))}
          >
            <ChevronRight size={16} />
          </button>
        </nav>
      </header>

      <div className="day-calendar-weekdays">
        {weekdays.map((weekday) => (
          <div key={weekday} className="day-calendar-weekday">
            {weekday}
          </div>
        ))}
      </div>

      <div className="day-calendar-weeks">
        {grid.weeks.map((week) => (
          <div key={week[0]!.date} className="day-calendar-week">
            {week.map((cell) => {
              const isSelected = cell.date === selectedDate;
              const isToday = cell.date === today;
              const hasNote = notedDates.has(cell.date);
              return (
                <button
                  key={cell.date}
                  type="button"
                  aria-label={formatDailyTitle(cell.date)}
                  aria-current={isToday ? "date" : undefined}
                  aria-pressed={isSelected}
                  onClick={() => onSelectDate(cell.date)}
                  className={[
                    "day-calendar-day",
                    !cell.inMonth && !isSelected && !isToday
                      ? "out-of-month"
                      : "",
                    isSelected ? "selected" : "",
                    isToday && !isSelected ? "today" : "",
                  ]
                    .filter(Boolean)
                    .join(" ")}
                >
                  {isSelected || isToday ? (
                    <span
                      aria-hidden
                      className={`day-calendar-mark ${isSelected ? "selected" : "today"}`}
                    />
                  ) : null}
                  {hasNote ? (
                    <span
                      aria-hidden
                      data-testid={`note-dot-${cell.date}`}
                      className="day-calendar-dot"
                    />
                  ) : null}
                  <span className="day-calendar-number">
                    {Number(cell.date.slice(8, 10))}
                  </span>
                </button>
              );
            })}
          </div>
        ))}
      </div>
    </div>
  );
}
