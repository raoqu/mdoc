export interface PublicationTarget {
  serverIP: string;
  sshUser: string;
  publishDirectory: string;
}

// Keep the existing deployTarget format for saved configurations and the CLI.
export function parsePublicationTarget(target: string): PublicationTarget {
  const separator = target.indexOf(":");
  const server = separator < 0 ? target : target.slice(0, separator);
  const userSeparator = server.indexOf("@");
  return {
    serverIP: userSeparator < 0 ? server : server.slice(userSeparator + 1),
    sshUser: userSeparator < 0 ? "" : server.slice(0, userSeparator),
    publishDirectory: separator < 0 ? "" : target.slice(separator + 1),
  };
}

export function formatPublicationTarget(target: PublicationTarget): string {
  const serverIP = target.serverIP.trim();
  const sshUser = target.sshUser.trim();
  const directory = target.publishDirectory.trim();
  if (!serverIP && !directory) return "";
  if (!serverIP || !directory) throw new Error("请同时填写 Server IP 和发布目录");
  if (!/^[a-zA-Z0-9][a-zA-Z0-9.-]*$/.test(serverIP)) {
    throw new Error("Server IP 请填写 IPv4 地址或主机名，不含协议、用户名和端口");
  }
  if (sshUser && !/^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$/.test(sshUser)) {
    throw new Error("SSH 用户名只能包含字母、数字、下划线、点和连字符");
  }
  return `${sshUser ? `${sshUser}@` : ""}${serverIP}:${directory}`;
}
