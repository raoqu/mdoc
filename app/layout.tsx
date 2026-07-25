import type { Metadata } from "next";
import "./globals.css";
import { PwaRegistration } from "./reflect/pwa-registration";

export const metadata: Metadata = {
  title: "墨笺 · 关联式 Markdown 笔记",
  description: "每日笔记、双向链接、任务与本地优先的 Markdown 知识库",
  manifest: "/manifest.webmanifest",
  applicationName: "墨笺",
  appleWebApp: {
    capable: true,
    statusBarStyle: "default",
    title: "墨笺",
  },
};
export const viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#f6f3ec" },
    { media: "(prefers-color-scheme: dark)", color: "#171712" },
  ],
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body>
        {children}
        <PwaRegistration />
      </body>
    </html>
  );
}
