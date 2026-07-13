import type { Metadata } from "next";
import "./globals.css";
import "./enhanced.css";

export const metadata: Metadata = { title: "墨笺 · Markdown 笔记", description: "安静、专注的 Markdown 笔记管理与静态发布工具" };
export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) { return <html lang="zh-CN"><body>{children}</body></html>; }
