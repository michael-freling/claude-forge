import { act, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useNavigate } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ScrollToAnchor } from "./ScrollToAnchor";

/** A button that navigates on click, to drive real hash changes. */
function Nav({ to }: { to: string }) {
  const navigate = useNavigate();
  return (
    <button type="button" onClick={() => navigate(to)}>
      go
    </button>
  );
}

function renderAt(initial: string, navTo = "") {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <ScrollToAnchor />
      {navTo && <Nav to={navTo} />}
    </MemoryRouter>,
  );
}

function addTarget(id: string): HTMLElement {
  const el = document.createElement("div");
  el.id = id;
  document.body.appendChild(el);
  return el;
}

describe("ScrollToAnchor", () => {
  const scrollIntoView = vi.fn();

  beforeEach(() => {
    vi.useFakeTimers();
    Element.prototype.scrollIntoView = scrollIntoView;
  });

  afterEach(() => {
    document
      .querySelectorAll("body > div[id]")
      .forEach((el) => el.remove());
    vi.clearAllMocks();
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("scrolls smoothly to the hash target and flashes it briefly", () => {
    const target = addTarget("sect");
    renderAt("/servers#sect");

    expect(scrollIntoView).toHaveBeenCalledWith({
      block: "start",
      behavior: "smooth",
    });
    expect(target).toHaveClass("anchor-flash");

    act(() => vi.advanceTimersByTime(1500));
    expect(target).not.toHaveClass("anchor-flash");
  });

  it("reacts to a hash change after mount", () => {
    const target = addTarget("later");
    renderAt("/sessions", "/sessions#later");
    expect(scrollIntoView).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "go" }));
    expect(scrollIntoView).toHaveBeenCalledTimes(1);
    expect(target).toHaveClass("anchor-flash");
  });

  it("skips smooth scrolling when the user prefers reduced motion", () => {
    vi.stubGlobal(
      "matchMedia",
      vi.fn(() => ({ matches: true })),
    );
    addTarget("calm");
    renderAt("/#calm");
    expect(scrollIntoView).toHaveBeenCalledWith({
      block: "start",
      behavior: "auto",
    });
  });

  it("polls for a target that renders after navigation (deep-link race)", () => {
    renderAt("/servers#slow");
    expect(scrollIntoView).not.toHaveBeenCalled();

    const target = addTarget("slow");
    act(() => vi.advanceTimersByTime(100));
    expect(scrollIntoView).toHaveBeenCalledTimes(1);
    expect(target).toHaveClass("anchor-flash");

    // the poll stops once found: no double scroll on later ticks
    act(() => vi.advanceTimersByTime(1000));
    expect(scrollIntoView).toHaveBeenCalledTimes(1);
  });

  it("gives up quietly when the target never appears", () => {
    renderAt("/servers#ghost");
    act(() => vi.advanceTimersByTime(3200));

    // even if the element shows up after the deadline, nothing happens
    addTarget("ghost");
    act(() => vi.advanceTimersByTime(1000));
    expect(scrollIntoView).not.toHaveBeenCalled();
  });

  it("does nothing without a hash and clears the flash on unmount", () => {
    const bare = renderAt("/sessions");
    expect(scrollIntoView).not.toHaveBeenCalled();
    bare.unmount();

    const target = addTarget("bye");
    const { unmount } = renderAt("/#bye");
    expect(target).toHaveClass("anchor-flash");
    unmount();
    expect(target).not.toHaveClass("anchor-flash");
  });
});
