export interface NoteTemplate {
  id: string;
  label: string;
  detail: string;
  markdown: string;
}

export interface StoredTemplate {
  id: string;
  notebookId: string;
  title: string;
  content: string;
  createdAt: string;
}

export const NOTE_TEMPLATES: NoteTemplate[] = [
  {
    id: "meeting",
    label: "会议记录",
    detail: "议程、讨论、决定和行动项",
    markdown:
      "## 议程\n\n- \n\n## 讨论\n\n\n## 决定\n\n- \n\n## 行动项\n\n+ [ ] ",
  },
  {
    id: "project",
    label: "项目计划",
    detail: "目标、范围、里程碑和风险",
    markdown:
      "## 目标\n\n\n## 范围\n\n\n## 里程碑\n\n+ [ ] \n\n## 风险\n\n- ",
  },
  {
    id: "reflection",
    label: "每日复盘",
    detail: "收获、阻碍和明日重点",
    markdown:
      "## 今天的收获\n\n- \n\n## 遇到的阻碍\n\n- \n\n## 明日重点\n\n+ [ ] ",
  },
];
