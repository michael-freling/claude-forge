import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { copyToClipboard } from "../../lib/clipboard";
import { CopyableId } from "./CopyableId";

vi.mock("../../lib/clipboard", () => ({
  copyToClipboard: vi.fn(() => Promise.resolve()),
}));

const mockedCopy = vi.mocked(copyToClipboard);
const FULL = "abcdef12-3456-7890";

beforeEach(() => {
  vi.useFakeTimers();
  mockedCopy.mockReset();
  mockedCopy.mockResolvedValue(undefined);
});
afterEach(() => vi.useRealTimers());

async function flush() {
  await act(async () => {
    await Promise.resolve();
  });
}

describe("CopyableId", () => {
  it("renders the first 8 characters as a real button", () => {
    render(<CopyableId id={FULL} />);
    const el = screen.getByRole("button");
    expect(el.tagName).toBe("BUTTON");
    expect(el).toHaveAttribute("type", "button");
    expect(el).toHaveTextContent("abcdef12");
  });

  it("copies on click, flashes 'copied', then reverts after 900ms", async () => {
    render(<CopyableId id={FULL} />);
    const el = screen.getByRole("button");

    fireEvent.click(el);
    await flush();

    expect(mockedCopy).toHaveBeenCalledWith(FULL);
    expect(el).toHaveTextContent("copied");
    expect(el).toHaveClass("copied");

    act(() => {
      vi.advanceTimersByTime(900);
    });
    expect(el).toHaveTextContent("abcdef12");
    expect(el).not.toHaveClass("copied");
  });

  it("clears a pending flash timer when copied again", async () => {
    render(<CopyableId id={FULL} />);
    const el = screen.getByRole("button");

    fireEvent.click(el);
    await flush();
    act(() => vi.advanceTimersByTime(400));
    fireEvent.click(el); // clears the first timer, starts a new one
    await flush();

    expect(el).toHaveTextContent("copied");
    expect(mockedCopy).toHaveBeenCalledTimes(2);
  });

  it("flashes 'copy failed' when the copy is rejected, then reverts", async () => {
    mockedCopy.mockRejectedValueOnce(new Error("denied"));
    render(<CopyableId id={FULL} />);
    const el = screen.getByRole("button");

    fireEvent.click(el);
    await flush();

    expect(el).toHaveTextContent("copy failed");
    expect(el).toHaveClass("failed");

    act(() => {
      vi.advanceTimersByTime(900);
    });
    expect(el).toHaveTextContent("abcdef12");
    expect(el).not.toHaveClass("failed");
  });
});
