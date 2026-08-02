/**
 * copyToClipboard writes `text` to the system clipboard. It prefers the async
 * Clipboard API (available in secure contexts) and falls back to a hidden
 * `<textarea>` + `document.execCommand("copy")` otherwise. The fallback
 * rejects when the copy command reports failure so callers can surface it.
 */
export function copyToClipboard(text: string): Promise<void> {
  if (navigator.clipboard && window.isSecureContext) {
    return navigator.clipboard.writeText(text);
  }
  return new Promise<void>((resolve, reject) => {
    try {
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      const ok = document.execCommand("copy");
      document.body.removeChild(ta);
      if (ok) {
        resolve();
      } else {
        reject(new Error("the copy command was rejected"));
      }
    } catch (e) {
      reject(e instanceof Error ? e : new Error(String(e)));
    }
  });
}
