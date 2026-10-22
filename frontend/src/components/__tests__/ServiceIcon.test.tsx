// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import ServiceIcon from "../ServiceIcon";

afterEach(cleanup);

it("renders Caddy as a local decorative service glyph without vendor image requests", () => {
  const { container } = render(<ServiceIcon name="caddy" />);
  expect(container.querySelector("img")).toBeNull();
  expect(container.querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
  expect(container.firstElementChild?.classList.contains("w-9")).toBe(true);
});
