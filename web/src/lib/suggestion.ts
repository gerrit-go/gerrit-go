// parseSuggestion extracts a ```suggestion fenced code block from a review
// comment message, returning the suggested replacement text (without the
// fences) or null when the message has no suggestion block.
export function parseSuggestion(message: string): string | null {
  const m = message.match(/```suggestion\s*\n([\s\S]*?)```/);
  if (!m) return null;
  // Trim a single trailing newline introduced by the closing fence.
  return m[1].replace(/\n$/, "");
}

// applySuggestionToLines replaces the 1-based line range [line, line+span-1]
// of the file content with the suggestion text, returning the new content.
// span defaults to 1 (the single commented line) since review comments carry a
// single line number.
export function applySuggestionToLines(content: string, line: number, suggestion: string, span = 1): string {
  const lines = content.split("\n");
  const start = Math.max(0, line - 1);
  const end = Math.min(lines.length, start + span);
  const replacement = suggestion === "" ? [] : suggestion.split("\n");
  lines.splice(start, end - start, ...replacement);
  return lines.join("\n");
}
