import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { copyToClipboard } from "../../lib/clipboard";
import { CopyableCommand } from "./CopyableCommand";

vi.mock("../../lib/clipboard", () => ({
  copyToClipboard: vi.fn(() => Promise.resolve()),
}));

const mockedCopy = vi.mocked(copyToClipboard);

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

describe("CopyableCommand", () => {
  it("renders the command and copies it on click, flashing 'Copied'", async () => {
    render(<CopyableCommand command="claude-forge start demo" />);
    expect(
      screen.getByText("claude-forge start demo", { selector: "code" }),
    ).toBeInTheDocument();

    const btn = screen.getByRole("button", { name: /Copy command/ });
    fireEvent.click(btn);
    await flush();

    expect(mockedCopy).toHaveBeenCalledWith("claude-forge start demo");
    expect(btn).toHaveTextContent("Copied");

    act(() => {
      vi.advanceTimersByTime(1200);
    });
    expect(btn).toHaveTextContent("Copy");
  });

  it("flashes 'Copy failed' when the copy rejects", async () => {
    mockedCopy.mockRejectedValueOnce(new Error("denied"));
    render(<CopyableCommand command="claude-forge start demo" />);

    const btn = screen.getByRole("button", { name: /Copy command/ });
    fireEvent.click(btn);
    await flush();

    expect(btn).toHaveTextContent("Copy failed");
    act(() => {
      vi.advanceTimersByTime(1200);
    });
    expect(btn).toHaveTextContent("Copy");
  });
});
