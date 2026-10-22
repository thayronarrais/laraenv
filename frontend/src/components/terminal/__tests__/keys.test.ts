import { describe, expect, it } from "vitest";
import { ctrlBackspaceSequence } from "../keys";

// Verified empirically against ConPTY (see commit message): \x08 is translated
// by conhost into a Ctrl+Backspace key event, which PSReadLine, cmd's cooked
// read and clink (Cmder) all treat as "delete previous word". bash/readline
// treats \x08 as a plain backspace, so bash-like shells get Ctrl+W instead.
describe("ctrlBackspaceSequence", () => {
  it("sends BS (\x08) to Windows-native shells so ConPTY sees Ctrl+Backspace", () => {
    expect(ctrlBackspaceSequence("powershell")).toBe("\x08");
    expect(ctrlBackspaceSequence("cmd")).toBe("\x08");
    expect(ctrlBackspaceSequence("cmder")).toBe("\x08");
  });

  it("sends Ctrl+W (\x17) to readline-style line editors", () => {
    expect(ctrlBackspaceSequence("gitbash")).toBe("\x17");
    expect(ctrlBackspaceSequence("ssh")).toBe("\x17");
    expect(ctrlBackspaceSequence("agent:claude")).toBe("\x17");
    expect(ctrlBackspaceSequence("agent:codex")).toBe("\x17");
  });

  it("falls back to BS for unknown or empty kinds (no regression vs today for cmd/pwsh)", () => {
    expect(ctrlBackspaceSequence("")).toBe("\x08");
    expect(ctrlBackspaceSequence("something-new")).toBe("\x08");
  });
});
