import { useEffect } from "react";
import { useLocation } from "react-router-dom";

/** How long the arrival highlight stays on the anchor target. */
const FLASH_MS = 1500;
/** How long to keep polling for a target that has not rendered yet. */
const RETRY_MS = 3000;
/** Polling interval while waiting for the target to render. */
const POLL_MS = 100;

/**
 * ScrollToAnchor makes URL hashes work under react-router, which does not
 * scroll to `#fragment` targets on client-side navigation. Mounted once in
 * App, it reacts to every hash change: it scrolls the target element into
 * view and briefly applies `.anchor-flash` so the eye lands on the right
 * section. Deep links race the initial data fetch, so a missing target is
 * polled for a few seconds before giving up. Smooth scrolling is skipped when
 * the user prefers reduced motion.
 */
export function ScrollToAnchor() {
  const { hash } = useLocation();

  useEffect(() => {
    if (!hash) {
      return;
    }
    const id = decodeURIComponent(hash.slice(1));
    let el: HTMLElement | null = null;
    let flashTimer: number | undefined;

    const tryScroll = (): boolean => {
      el = document.getElementById(id);
      if (!el) {
        return false;
      }
      const reduced =
        window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ??
        false;
      el.scrollIntoView?.({
        block: "start",
        behavior: reduced ? "auto" : "smooth",
      });
      el.classList.add("anchor-flash");
      flashTimer = window.setTimeout(
        () => el?.classList.remove("anchor-flash"),
        FLASH_MS,
      );
      return true;
    };

    let poll: number | undefined;
    if (!tryScroll()) {
      // The target may not exist yet (the snapshot is still loading on a
      // deep link): retry briefly, then give up quietly.
      const deadline = Date.now() + RETRY_MS;
      poll = window.setInterval(() => {
        if (tryScroll() || Date.now() > deadline) {
          window.clearInterval(poll);
        }
      }, POLL_MS);
    }
    return () => {
      window.clearInterval(poll);
      window.clearTimeout(flashTimer);
      el?.classList.remove("anchor-flash");
    };
  }, [hash]);

  return null;
}
