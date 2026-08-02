import { useCallback, useEffect, useRef, useState } from "react";
import { copyToClipboard } from "../../lib/clipboard";
import { shortId } from "../../lib/format";
import { CopyIcon } from "./icons";

type CopyState = "idle" | "copied" | "failed";

/**
 * CopyableId renders the first 8 chars of an id as a real button that copies
 * the full id on activation, flashing "copied" (or "copy failed") for ~900ms.
 * The flash is announced politely to screen readers; a reserved min-width
 * keeps the label swap from shifting the table layout.
 */
export function CopyableId({ id }: { id: string }) {
  const [state, setState] = useState<CopyState>("idle");
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const flash = useCallback((next: CopyState) => {
    setState(next);
    if (timer.current) {
      clearTimeout(timer.current);
    }
    timer.current = setTimeout(() => setState("idle"), 900);
  }, []);

  const doCopy = useCallback(() => {
    copyToClipboard(id)
      .then(() => flash("copied"))
      .catch(() => flash("failed"));
  }, [id, flash]);

  useEffect(
    () => () => {
      if (timer.current) {
        clearTimeout(timer.current);
      }
    },
    [],
  );

  return (
    <button
      type="button"
      className={"sid mono" + (state === "idle" ? "" : " " + state)}
      title={id + "  (click to copy)"}
      onClick={doCopy}
    >
      <span aria-live="polite">
        {state === "idle"
          ? shortId(id)
          : state === "copied"
            ? "copied"
            : "copy failed"}
      </span>
      <CopyIcon className="sid-glyph" size={12} />
    </button>
  );
}
