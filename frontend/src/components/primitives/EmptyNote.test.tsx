import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { EmptyNote } from "./EmptyNote";

describe("EmptyNote", () => {
  it("renders its children in a muted note", () => {
    const { container } = render(<EmptyNote>Nothing here.</EmptyNote>);
    expect(container.querySelector(".empty-note")).toHaveTextContent(
      "Nothing here.",
    );
    expect(screen.getByText("Nothing here.")).toBeInTheDocument();
  });
});
