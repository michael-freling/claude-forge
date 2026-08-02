import { RelativeTime } from "./primitives/RelativeTime";

/** Header is the sticky top bar: brand, "generated" time, and Refresh action. */
export function Header({
  generatedAt,
  loading,
  onRefresh,
}: {
  generatedAt?: Date;
  loading: boolean;
  onRefresh: () => void;
}) {
  return (
    <header className="topbar">
      <div className="brand">
        <span className="mark" aria-hidden="true">
          ⚒
        </span>
        <span className="title">claude-forge</span>
        <span className="sub">dashboard</span>
      </div>
      <div className="actions">
        {generatedAt && (
          <span className="gen">
            generated <RelativeTime value={generatedAt} />
          </span>
        )}
        <button
          className="btn btn-primary"
          onClick={onRefresh}
          disabled={loading}
        >
          {loading ? "Refreshing…" : "Refresh"}
        </button>
      </div>
    </header>
  );
}
