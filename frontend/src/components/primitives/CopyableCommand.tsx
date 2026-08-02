import { useCallback, useEffect, useRef, useState } from "react";
import { copyToClipboard } from "../../lib/clipboard";
import { CopyIcon } from "./icons";

type CopyState = "idle" | "copied" | "failed";

/**
 * CopyableCommand renders a shell command as code with a Copy button, flashing
 * "Copied"/"Copy failed" for ~1.2s. The flash is announced politely.
 */
export function CopyableCommand({ command }: { command: string }) {
  const [state, setState] = useState<CopyState>("idle");
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const doCopy = useCallback(() => {
    copyToClipboard(command)
      .then(() => setState("copied"))
      .catch(() => setState("failed"))
      .finally(() => {
        if (timer.current) {
          clearTimeout(timer.current);
        }
        timer.current = setTimeout(() => setState("idle"), 1200);
      });
  }, [command]);

  useEffect(
    () => () => {
      if (timer.current) {
        clearTimeout(timer.current);
      }
    },
    [],
  );

  return (
    <div className="cmd">
      <code className="mono">{command}</code>
      <button
        type="button"
        className="btn cmd-copy"
        onClick={doCopy}
        aria-label={`Copy command: ${command}`}
      >
        <CopyIcon size={13} />
        <span aria-live="polite">
          {state === "idle" ? "Copy" : state === "copied" ? "Copied" : "Copy failed"}
        </span>
      </button>
    </div>
  );
}
