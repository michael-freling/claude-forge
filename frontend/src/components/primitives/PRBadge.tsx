import type { PullRequest } from "../../gen/dashboard/v1/dashboard_pb";
import { isSafeHref, prStateClass, prStateLabel } from "../../lib/format";

/**
 * PRBadge renders a pull-request chip coloured by state (open=green,
 * merged=purple, closed=red) with a `draft` tag when applicable. It renders a
 * real link ONLY when the PR url passes {@link isSafeHref}; otherwise it renders
 * an inert `<span>` so a hostile `javascript:`/`data:` url can never navigate.
 */
export function PRBadge({ pr }: { pr?: PullRequest }) {
  if (!pr) {
    return <span className="faint">—</span>;
  }

  const cls = prStateClass(pr.state);
  const label = prStateLabel(pr.state);
  const title =
    `#${pr.number} · ${label}` +
    (pr.draft ? " · draft" : "") +
    (pr.title ? ` — ${pr.title}` : "");

  const inner = (
    <>
      <span className="pr-num">#{pr.number}</span>
      <span className="pr-title">{pr.title}</span>
      {pr.draft && <span className="tag">draft</span>}
    </>
  );

  if (isSafeHref(pr.url)) {
    return (
      <a
        className={"pr " + cls}
        title={title}
        href={pr.url}
        target="_blank"
        rel="noopener noreferrer"
      >
        {inner}
      </a>
    );
  }

  return (
    <span className={"pr " + cls} title={title}>
      {inner}
    </span>
  );
}
