import { describe, expect, it } from "vitest";
import { addTag, canonicalTag, suggestTags } from "../tags";

const existing = ["APEC", "Biquara", "Clients", "Fengshui", "FullWear", "HDV", "Personal", "Testing"];

describe("suggestTags", () => {
  it("lists every existing tag not yet selected when the draft is empty", () => {
    expect(suggestTags(existing, ["HDV"], "")).toEqual(
      ["APEC", "Biquara", "Clients", "Fengshui", "FullWear", "Personal", "Testing"],
    );
  });
  it("matches case-insensitively, prefix matches before substring matches", () => {
    expect(suggestTags(existing, [], "fu")).toEqual(["FullWear"]);
    expect(suggestTags(existing, [], "en")).toEqual(["Clients", "Fengshui"]);
  });
  it("never suggests a tag that is already selected", () => {
    expect(suggestTags(existing, ["fullwear"], "full")).toEqual([]);
  });
  it("respects the limit", () => {
    expect(suggestTags(existing, [], "", 3)).toEqual(["APEC", "Biquara", "Clients"]);
  });
  it("ignores blank existing entries and trims the draft", () => {
    expect(suggestTags(["  ", "HDV"], [], "  hd ")).toEqual(["HDV"]);
  });
});

describe("canonicalTag", () => {
  it("reuses the existing spelling when the draft only differs by case", () => {
    expect(canonicalTag(existing, "fullwear")).toBe("FullWear");
    expect(canonicalTag(existing, " hdv ")).toBe("HDV");
  });
  it("keeps a brand new tag as typed", () => {
    expect(canonicalTag(existing, "Prod")).toBe("Prod");
  });
});

describe("addTag", () => {
  it("dedupes case-insensitively", () => {
    expect(addTag(["HDV"], "hdv")).toEqual(["HDV"]);
    expect(addTag(["HDV"], " Prod ")).toEqual(["HDV", "Prod"]);
  });
});
