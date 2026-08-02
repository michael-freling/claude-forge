import { useCallback, useEffect, useRef, useState } from "react";
import { copyToClipboard } from "../../lib/clipboard";
import { shortId } from "../../lib/format";

/**
 * CopyableId renders the first 8 chars of an id as a keyboard-focusable
 * "button" that copies the full id on click / Enter / Space, flashing "copied"
 * for ~900ms.
 */
export function CopyableId({ id }: { id: string }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const doCopy = useCallback(() => {
    copyToClipboard(id)
      .then(() => {
        setCopied(true);
        if (timer.current) {
          clearTimeout(timer.current);
        }
        timer.current = setTimeout(() => setCopied(false), 900);
      })
      .catch(() => {
        /* copy failed — leave the id untouched */
      });
  }, [id]);

  useEffect(
    () => () => {
      if (timer.current) {
        clearTimeout(timer.current);
      }
    },
    [],
  );

  return (
    <span
      className={"sid mono" + (copied ? " copied" : "")}
      role="button"
      tabIndex={0}
      title={id + "  (click to copy)"}
      onClick={doCopy}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          doCopy();
        }
      }}
    >
      {copied ? "copied" : shortId(id)}
    </span>
  );
}
