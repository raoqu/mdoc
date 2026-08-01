export interface DocumentRecord {
  id: string;
  title: string;
  content: string;
  updatedAt: string;
  createdAt?: string;
  pinned?: boolean;
  trashed?: boolean;
  private?: boolean;
  aliases?: string[];
  revision: number;
}

export interface FolderRecord {
  id: string;
  title: string;
  open: boolean;
  docs: DocumentRecord[];
  children: FolderRecord[];
}

export interface NotebookRecord {
  id: string;
  title: string;
  description: string;
  accent: string;
  folders: FolderRecord[];
}

export interface DocumentLocation {
  document: DocumentRecord;
  folder: FolderRecord;
  notebook: NotebookRecord;
}

export type SettingsSection =
  | "general"
  | "models"
  | "ai-chat"
  | "assets"
  | "templates"
  | "capture"
  | "sync"
  | "data";

export type WorkspaceView =
  | { kind: "daily"; date: string }
  | { kind: "note"; documentId: string }
  | { kind: "all-notes" }
  | { kind: "tasks" }
  | { kind: "trash" }
  | { kind: "tag"; tag: string }
  | { kind: "chat"; conversationId?: string }
  | { kind: "settings"; section?: SettingsSection };

export type MarkdownSyntaxMode = "hide" | "focus" | "show";

export interface WorkspaceSettings {
  theme: "light" | "dark" | "system";
  syntaxMode: MarkdownSyntaxMode;
  spellCheck: boolean;
  startWithBullet: boolean;
  editorWidth: "reading" | "wide";
  textSize: "small" | "medium" | "large";
  describeAssets: boolean;
  semanticSearchEnabled: boolean;
  chatModelSelection: { configId: string; modelId: string } | null;
  chatSystemPrompt: string;
}

export const DEFAULT_SETTINGS: WorkspaceSettings = {
  theme: "system",
  syntaxMode: "hide",
  spellCheck: true,
  startWithBullet: false,
  editorWidth: "reading",
  textSize: "medium",
  describeAssets: true,
  semanticSearchEnabled: false,
  chatModelSelection: null,
  chatSystemPrompt: "",
};

export function documentsInFolders(
  folders: readonly FolderRecord[],
  notebook: NotebookRecord,
): DocumentLocation[] {
  return folders.flatMap((folder) => [
    ...folder.docs.map((document) => ({ document, folder, notebook })),
    ...documentsInFolders(folder.children, notebook),
  ]);
}

export function documentsInNotebook(notebook: NotebookRecord): DocumentLocation[] {
  return documentsInFolders(notebook.folders, notebook);
}

export function updateFolders(
  folders: readonly FolderRecord[],
  update: (folder: FolderRecord) => FolderRecord,
): FolderRecord[] {
  return folders.map((folder) =>
    update({
      ...folder,
      children: updateFolders(folder.children, update),
    }),
  );
}

export function updateDocument(
  notebooks: readonly NotebookRecord[],
  documentId: string,
  update: (document: DocumentRecord) => DocumentRecord,
): NotebookRecord[] {
  return notebooks.map((notebook) => ({
    ...notebook,
    folders: updateFolders(notebook.folders, (folder) => ({
      ...folder,
      docs: folder.docs.map((document) =>
        document.id === documentId ? update(document) : document,
      ),
    })),
  }));
}

export function removeDocument(
  notebooks: readonly NotebookRecord[],
  documentId: string,
): NotebookRecord[] {
  return notebooks.map((notebook) => ({
    ...notebook,
    folders: updateFolders(notebook.folders, (folder) => ({
      ...folder,
      docs: folder.docs.filter((document) => document.id !== documentId),
    })),
  }));
}
