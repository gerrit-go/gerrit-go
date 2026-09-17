import { useEffect } from "react";

// useHotkey registers a global keydown handler. It ignores keypresses while
// the user is typing in an input/textarea/contenteditable or when a modifier
// (Ctrl/Meta/Alt) is held, so plain-letter shortcuts don't fight normal typing.
export function useHotkey(handler: (e: KeyboardEvent) => void) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey) return;
      const target = e.target as HTMLElement | null;
      if (target) {
        const tag = target.tagName;
        if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || target.isContentEditable) {
          return;
        }
      }
      handler(e);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [handler]);
}
