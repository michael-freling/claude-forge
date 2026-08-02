/** WorktreeBadge renders a session's worktree name, or "—" when absent. */
export function WorktreeBadge({ worktree }: { worktree: string }) {
  if (!worktree) {
    return <span className="faint">—</span>;
  }
  return (
    <span className="badge wt mono" title={worktree}>
      {worktree}
    </span>
  );
}
