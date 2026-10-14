export interface PublicationConfig {
  theme?: string;
  treeTheme?: string;
  title: string;
  baseURL: string;
  subtitle: string;
  themeModule: string;
  themeVersion: string;
  includeTOC: boolean;
  deployTarget: string;
}

export interface PublicationStatus {
  themes?: { id: string; label: string; module?: string; version?: string; treeTheme?: string }[];
  config: PublicationConfig;
  documents: { id: string; title: string; url: string }[];
  themeInstalled: boolean;
  directory: string;
  previewURL: string;
}

export interface PublicationResult {
  log: string;
  documents: number;
}

export interface PublicationDeployInput {
  directory: string;
  config: PublicationConfig;
}

export interface PublicationDeployment {
  status: PublicationStatus;
  result: PublicationResult;
}

export interface PublicationPreview {
  status: PublicationStatus;
  result: PublicationResult;
}

export class PublicationSetupError extends Error {}

export async function publicationResponseJSON<T>(response: Response): Promise<T> {
  if (!response.ok) throw new Error((await response.text()).trim());
  return response.json() as Promise<T>;
}

export async function deployPublication(input?: PublicationDeployInput): Promise<PublicationDeployment> {
  const status = await publicationResponseJSON<PublicationStatus>(await fetch("/api/publication", input ? {
    method: "PUT",
    headers: { "Content-Type": "application/json", "X-Mdoc-Publication-Directory": input.directory },
    body: JSON.stringify(input.config),
  } : undefined));
  if (!status.config.deployTarget.trim()) {
    throw new PublicationSetupError("请先填写并保存 Server IP 和发布目录");
  }
  if (!status.themeInstalled) {
    throw new PublicationSetupError("请先在发布配置中安装所选主题");
  }
  const result = await publicationResponseJSON<PublicationResult>(await fetch("/api/publication/deploy", {
    method: "POST",
    headers: { "X-Mdoc-Publication-Directory": status.directory },
  }));
  return { status, result };
}

export async function previewPublication(): Promise<PublicationPreview> {
  const status = await publicationResponseJSON<PublicationStatus>(await fetch("/api/publication"));
  if (!status.themeInstalled) {
    throw new PublicationSetupError("请先在发布配置中安装所选主题");
  }
  const result = await publicationResponseJSON<PublicationResult>(await fetch("/api/publication/build", {
    method: "POST",
    headers: { "X-Mdoc-Publication-Directory": status.directory },
  }));
  return { status, result };
}
