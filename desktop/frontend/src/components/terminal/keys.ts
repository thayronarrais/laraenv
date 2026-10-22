// Keyboard sequences xterm.js does not produce on its own.
//
// xterm.js 5.x emits DEL (\x7f) for both Backspace and Ctrl+Backspace, so the
// shell never learns Ctrl was held and deletes a single character. Windows
// Terminal instead sends BS (\x08) for Ctrl+Backspace; ConPTY (conhost)
// translates that byte into a VK_BACK key event with the Ctrl modifier, which
// PSReadLine (BackwardKillWord), cmd's cooked read and clink (Cmder) all treat
// as "delete previous word".
//
// bash/readline treats \x08 as a plain backspace, so bash-like line editors
// (Git Bash, remote shells over SSH, agent CLIs such as Claude Code) get
// Ctrl+W (\x17) — unix-word-rubout — which bash, zsh, fish and Ink/bubbletea
// based TUIs all bind to word deletion.

const READLINE_STYLE_KINDS = new Set(["gitbash", "ssh"]);

/**
 * Returns the byte sequence to write to the PTY when the user presses
 * Ctrl+Backspace in a pane whose leaf `kind` is the given value
 * ("powershell" | "cmd" | "gitbash" | "cmder" | "ssh" | "agent:<id>").
 */
export function ctrlBackspaceSequence(kind: string): string {
  if (READLINE_STYLE_KINDS.has(kind) || kind.startsWith("agent:")) return "\x17";
  return "\x08";
}
