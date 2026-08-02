import { NavLink } from "react-router-dom";
import { ForgeMarkIcon } from "./primitives/icons";
import { RelativeTime } from "./primitives/RelativeTime";

const tabClass = ({ isActive }: { isActive: boolean }) =>
  "tab" + (isActive ? " active" : "");

/**
 * Header is the sticky top bar: brand, the Running/Sessions/Servers nav tabs,
 * "generated" time, and the Refresh action. While a refresh is in flight the
 * button reports busy via `aria-busy`/`aria-disabled` (never the `disabled`
 * attribute, which would drop keyboard focus to the body mid-refresh).
 */
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
          <ForgeMarkIcon size={17} />
        </span>
        <span className="title">claude-forge</span>
        <span className="sub">dashboard</span>
      </div>
      <nav className="tabs" aria-label="Dashboard sections">
        <NavLink to="/" end className={tabClass}>
          Running
        </NavLink>
        <NavLink to="/sessions" className={tabClass}>
          Sessions
        </NavLink>
        <NavLink to="/servers" className={tabClass}>
          Servers
        </NavLink>
      </nav>
      <div className="actions">
        {generatedAt && (
          <span className="gen">
            generated <RelativeTime value={generatedAt} />
          </span>
        )}
        <button
          type="button"
          className="btn btn-primary"
          onClick={() => {
            if (!loading) {
              onRefresh();
            }
          }}
          aria-busy={loading}
          aria-disabled={loading}
        >
          {loading ? "Refreshing…" : "Refresh"}
        </button>
      </div>
    </header>
  );
}
