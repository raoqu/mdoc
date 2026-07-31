export interface PositionedChatTool {
  toolCallId: string;
  textOffset?: number;
}

export type ChatTranscriptPart<T extends PositionedChatTool> =
  | { kind: "text"; text: string }
  | { kind: "tool"; activity: T };

export function interleaveChatTools<T extends PositionedChatTool>(
  text: string,
  tools: readonly T[],
): ChatTranscriptPart<T>[] {
  const characters = Array.from(text);
  const positioned = tools
    .map((activity, index) => ({
      activity,
      index,
      offset: Math.min(
        Math.max(activity.textOffset ?? 0, 0),
        characters.length,
      ),
    }))
    .sort(
      (left, right) =>
        left.offset - right.offset || left.index - right.index,
    );
  const parts: ChatTranscriptPart<T>[] = [];
  let cursor = 0;

  for (const { activity, offset } of positioned) {
    if (offset > cursor) {
      parts.push({
        kind: "text",
        text: characters.slice(cursor, offset).join(""),
      });
    }
    parts.push({ kind: "tool", activity });
    cursor = offset;
  }
  if (cursor < characters.length) {
    parts.push({
      kind: "text",
      text: characters.slice(cursor).join(""),
    });
  }
  return parts;
}
