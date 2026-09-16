import hljs from "highlight.js/lib/common";

// Map a file path to a highlight.js language id from its extension. Returns
// undefined when the extension is unknown so callers can skip highlighting.
const EXT_TO_LANG: Record<string, string> = {
  go: "go",
  js: "javascript", jsx: "javascript", mjs: "javascript", cjs: "javascript",
  ts: "typescript", tsx: "typescript",
  py: "python", pyw: "python",
  java: "java", kt: "kotlin", kts: "kotlin", scala: "scala",
  c: "c", h: "c", cc: "cpp", cpp: "cpp", cxx: "cpp", hh: "cpp", hpp: "cpp",
  cs: "csharp",
  rs: "rust",
  rb: "ruby",
  php: "php",
  sh: "bash", bash: "bash", zsh: "bash",
  sql: "sql",
  html: "xml", htm: "xml", xml: "xml", svg: "xml", xhtml: "xml",
  css: "css", scss: "scss", less: "less",
  json: "json", jsonc: "json",
  yaml: "yaml", yml: "yaml",
  toml: "ini", ini: "ini", cfg: "ini",
  md: "markdown", markdown: "markdown",
  mk: "makefile", mak: "makefile", makefile: "makefile",
  gradle: "groovy", groovy: "groovy",
  dockerfile: "dockerfile",
  proto: "protobuf",
  lua: "lua",
  perl: "perl", pl: "perl",
  swift: "swift",
  r: "r",
  dart: "dart",
  ex: "elixir", exs: "elixir",
  erl: "erlang", hrl: "erlang",
  hs: "haskell",
  clj: "clojure",
  vim: "vim",
  diff: "diff", patch: "diff",
};

const SPECIAL_FILENAMES: Record<string, string> = {
  dockerfile: "dockerfile",
  makefile: "makefile",
  "cmakelists.txt": "cmake",
};

export function langForPath(path: string): string | undefined {
  const base = path.split("/").pop()?.toLowerCase() ?? "";
  if (SPECIAL_FILENAMES[base]) return SPECIAL_FILENAMES[base];
  const dot = base.lastIndexOf(".");
  if (dot < 0) return undefined;
  const ext = base.slice(dot + 1);
  return EXT_TO_LANG[ext];
}

// highlightLine highlights a single source line, returning HTML. Falls back to
// escaped plain text when the language is unknown or highlighting fails. The
// caller must render the result with dangerouslySetInnerHTML.
export function highlightLine(text: string, lang?: string): string {
  if (!lang || !hljs.getLanguage(lang)) {
    return escapeHtml(text);
  }
  try {
    return hljs.highlight(text, { language: lang, ignoreIllegals: true }).value;
  } catch {
    return escapeHtml(text);
  }
}

// highlightBlock highlights a whole file at once, preserving cross-line tokens
// (multi-line strings/comments) that per-line highlighting would break.
export function highlightBlock(text: string, lang?: string): string {
  if (!lang || !hljs.getLanguage(lang)) {
    return escapeHtml(text);
  }
  try {
    return hljs.highlight(text, { language: lang, ignoreIllegals: true }).value;
  } catch {
    return escapeHtml(text);
  }
}

export function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}
