import {
  checkRoundTrip,
  docToMarkdown,
  markdownToDoc,
  type RoundTripFidelity,
} from "@meowdown/core";

export function checkEditorRoundTrip(markdown: string): RoundTripFidelity {
  const fidelity = checkRoundTrip(markdown);
  if (fidelity !== "lossy") return fidelity;

  // An unfinished code fence is valid while typing. Only allow it when adding
  // the serializer's closing fence makes the entire document round-trip safely.
  // Reuse its fence width, marker and container prefix for lists and quotes.
  const closingFence = docToMarkdown(markdownToDoc(markdown))
    .trimEnd()
    .split("\n")
    .at(-1);
  if (!closingFence || !/^[\t >]*(?:`{3,}|~{3,})[\t ]*$/.test(closingFence)) {
    return fidelity;
  }

  const completed = `${markdown}${markdown.endsWith("\n") ? "" : "\n"}${closingFence}`;
  return checkRoundTrip(completed) === "lossy" ? "lossy" : "normalizing";
}
