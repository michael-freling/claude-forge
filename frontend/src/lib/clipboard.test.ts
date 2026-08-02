import { afterEach, describe, expect, it, vi } from "vitest";
import { copyToClipboard } from "./clipboard";

function define(target: object, prop: string, value: unknown) {
  Object.defineProperty(target, prop, {
    value,
    configurable: true,
    writable: true,
  });
}
const setClipboard = (v: unknown) => define(navigator, "clipboard", v);
const setSecure = (v: boolean) => define(window, "isSecureContext", v);
const setExecCommand = (fn: () => boolean) =>
  define(document, "execCommand", fn);

afterEach(() => {
  setClipboard(undefined);
  setSecure(false);
  vi.restoreAllMocks();
});

describe("copyToClipboard", () => {
  it("uses the async Clipboard API in a secure context", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    setClipboard({ writeText });
    setSecure(true);

    await copyToClipboard("hello");
    expect(writeText).toHaveBeenCalledWith("hello");
  });

  it("falls back to execCommand when not in a secure context", async () => {
    const writeText = vi.fn();
    setClipboard({ writeText });
    setSecure(false);
    const exec = vi.fn(() => true);
    setExecCommand(exec);

    await copyToClipboard("fallback");

    expect(writeText).not.toHaveBeenCalled();
    expect(exec).toHaveBeenCalledWith("copy");
    // the temporary textarea is removed again
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("falls back to execCommand when the Clipboard API is absent", async () => {
    setClipboard(undefined);
    setSecure(true);
    const exec = vi.fn(() => true);
    setExecCommand(exec);

    await expect(copyToClipboard("x")).resolves.toBeUndefined();
    expect(exec).toHaveBeenCalledWith("copy");
  });

  it("rejects with an Error when execCommand throws an Error", async () => {
    setClipboard(undefined);
    setSecure(false);
    setExecCommand(() => {
      throw new Error("denied");
    });

    await expect(copyToClipboard("x")).rejects.toThrow("denied");
  });

  it("wraps a non-Error throw in an Error", async () => {
    setClipboard(undefined);
    setSecure(false);
    setExecCommand(() => {
      throw "nope";
    });

    await expect(copyToClipboard("x")).rejects.toThrow("nope");
  });
});
