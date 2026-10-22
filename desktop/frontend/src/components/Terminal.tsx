import { useEffect, useRef } from "react";
import { Terminal as XTerm } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import { WebglAddon } from "xterm-addon-webgl";
import "xterm/css/xterm.css";
import { tabbyTheme, TERMINAL_FONT } from "./terminal/tabbyTheme";
// @ts-ignore
import { WritePty, ResizePty } from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn, EventsOff } from "../../wailsjs/runtime";
import {
  getTerminalCopyMode,
  onTerminalCopyModeChange,
  TerminalCopyMode,
} from "../terminalPrefs";
import { isEditingElsewhere, noteTerminalInput } from "./agents/terminalActivity";
import { readTerminalClipboard, writeTerminalClipboard } from "./terminal/clipboard";

export type TerminalErrorHandler = (operation: "copy" | "paste" | "input", error: unknown) => void;

// A single xterm instance bound to an existing PTY session id created on the
// backend. Multiple instances coexist; each listens on its own event channel
// "term:data:<id>" / "term:exit:<id>".
//
// `ctrlBackspace` is the byte sequence written when Ctrl+Backspace is pressed
// (see terminal/keys.ts); xterm.js alone would send a plain DEL.
export default function Term({
  id,
  visible,
  focused = visible,
  focusRequest,
  ctrlBackspace = "\x08",
  onExit,
  onFocus,
  onError,
}: {
  id: string;
  visible: boolean;
  focused?: boolean;
  focusRequest?: number;
  ctrlBackspace?: string;
  onFocus?: () => void;
  onError?: TerminalErrorHandler;
  /**
   * The PTY behind this pane is gone — the shell was exited, or the agent
   * session driving it was stopped from the Agents page. Without this the
   * pane stayed on screen, alive-looking and inert.
   */
  onExit?: (reason: string) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const termRef = useRef<XTerm | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const openedRef = useRef(false);
  const focusRef = useRef({ visible, focused });
  focusRef.current = { visible, focused };
  // Read through a ref so the key handler (registered once) always sees the
  // latest sequence without re-creating the xterm instance.
  const ctrlBackspaceRef = useRef(ctrlBackspace);
  ctrlBackspaceRef.current = ctrlBackspace;
  const onExitRef = useRef(onExit);
  onExitRef.current = onExit;
  const onFocusRef = useRef(onFocus);
  onFocusRef.current = onFocus;
  const onErrorRef = useRef(onError);
  onErrorRef.current = onError;

  useEffect(() => {
    let disposed = false;
    const term = new XTerm({
      ...TERMINAL_FONT,
      cursorBlink: true,
      cursorStyle: "block",
      allowTransparency: false,
      scrollback: 5000,
      theme: tabbyTheme,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    termRef.current = term;
    fitRef.current = fit;

    const sendInput = async (data: string) => {
      if (disposed) return;
      noteTerminalInput(id, data);
      try {
        await WritePty(id, data);
      } catch (error) {
        if (!disposed) onErrorRef.current?.("input", error);
      }
    };
    const onData = term.onData((data) => { void sendInput(data); });

    const copy = async (clearSelection: boolean) => {
      const selection = term.getSelection();
      if (!selection) return;
      try {
        await writeTerminalClipboard(selection);
        if (!disposed && clearSelection && term.getSelection() === selection) term.clearSelection();
      } catch (error) {
        if (!disposed) onErrorRef.current?.("copy", error);
      }
    };
    const paste = async () => {
      try {
        const text = await readTerminalClipboard();
        // paste() applies newline/bracketed-paste handling and emits onData,
        // sharing the same agent activity and error handling as keyboard input.
        if (!disposed && openedRef.current && text) term.paste(text);
      } catch (error) {
        if (!disposed) onErrorRef.current?.("paste", error);
      }
    };

    const dataEvt = `term:data:${id}`;
    const exitEvt = `term:exit:${id}`;
    EventsOn(dataEvt, (chunk: string) => term.write(chunk));
    EventsOn(exitEvt, (msg: string) => {
      // Read through a ref: this listener is registered once, and the owner
      // re-renders often enough that a captured callback would go stale.
      onExitRef.current?.(msg);
    });

    const modeRef: { current: TerminalCopyMode } = {
      current: getTerminalCopyMode(),
    };
    const offModeChange = onTerminalCopyModeChange((m) => {
      modeRef.current = m;
    });

    term.attachCustomKeyEventHandler((e) => {
      if (
        e.type === "keydown" &&
        e.key === "Backspace" &&
        e.ctrlKey &&
        !e.shiftKey &&
        !e.altKey &&
        !e.metaKey
      ) {
        // xterm.js maps Ctrl+Backspace to DEL (single char); send the shell
        // the word-delete sequence it understands instead.
        e.preventDefault();
        e.stopPropagation();
        void sendInput(ctrlBackspaceRef.current);
        return false;
      }
      // Mouse mode adds right-click copy/paste; dedicated shortcuts stay usable.
      if (e.type !== "keydown" || !e.ctrlKey || !e.shiftKey || e.altKey || e.metaKey) return true;
      const key = e.key.toLowerCase();
      if (key !== "c" && key !== "v") return true;
      // Returning false only stops xterm. Cancel the browser action too, or a
      // native paste event can insert the same clipboard text a second time.
      e.preventDefault();
      e.stopPropagation();
      if (key === "c") void copy(false);
      else void paste();
      return false;
    });

    const onCtx = (e: MouseEvent) => {
      if (modeRef.current !== "mouse") return;
      e.preventDefault();
      e.stopPropagation();
      term.focus();
      if (term.hasSelection()) void copy(true);
      else void paste();
    };
    // Capture before xterm moves/selects its helper textarea for a native menu.
    ref.current!.addEventListener("contextmenu", onCtx, true);
    const ctxHost = ref.current;

    const onResize = () => {
      if (disposed || !openedRef.current || !focusRef.current.visible) return;
      try {
        fit.fit();
        ResizePty(id, term.cols, term.rows);
      } catch {}
    };
    window.addEventListener("resize", onResize);
    const ro = new ResizeObserver(onResize);

    // Open the terminal only once the webfont is available. xterm measures the
    // cell width at open() time; opening before "JetBrains Mono" has loaded
    // would size columns against the fallback font and shift everything on the
    // first resize. Everything that needs a live screen (open, WebGL renderer,
    // fit, resize observer) happens inside this callback.
    let webgl: WebglAddon | null = null;
    let resizeTimer: ReturnType<typeof setTimeout> | undefined;
    document.fonts.load(`${TERMINAL_FONT.fontSize}px "JetBrains Mono"`).then(() => {
      if (disposed || !ref.current) return;
      term.open(ref.current);
      openedRef.current = true;
      // GPU renderer for crisp text; fall back to the DOM renderer if WebGL is
      // unavailable or its context is lost.
      try {
        webgl = new WebglAddon();
        webgl.onContextLoss(() => {
          webgl?.dispose();
          webgl = null;
        });
        term.loadAddon(webgl);
      } catch {
        webgl = null;
      }
      onResize();
      if (focusRef.current.visible && focusRef.current.focused && !isEditingElsewhere()) term.focus();
      ro.observe(ref.current);
      // Initial size sync
      resizeTimer = setTimeout(onResize, 50);
    });

    return () => {
      disposed = true;
      openedRef.current = false;
      clearTimeout(resizeTimer);
      window.removeEventListener("resize", onResize);
      ro.disconnect();
      onData.dispose();
      EventsOff(dataEvt);
      EventsOff(exitEvt);
      offModeChange();
      ctxHost?.removeEventListener("contextmenu", onCtx, true);
      webgl?.dispose();
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
  }, [id]);

  // When the tab becomes visible again, refit (xterm sizes wrong while hidden).
  useEffect(() => {
    if (visible && fitRef.current && termRef.current) {
      const focusWhenScheduled = document.activeElement;
      const timer = setTimeout(() => {
        if (!openedRef.current || !focusRef.current.visible) return;
        try {
          fitRef.current!.fit();
          ResizePty(id, termRef.current!.cols, termRef.current!.rows);
          // A modal may restore its opener after this effect schedules work.
          // Refitting must not override that newer focus owner. A later explicit
          // request schedules fresh work and can still focus this terminal.
          const active = document.activeElement;
          const focusMovedElsewhere = active !== focusWhenScheduled && active !== document.body
            && active !== null && !ref.current?.contains(active);
          if (focusRef.current.focused && !focusMovedElsewhere && !isEditingElsewhere()) termRef.current!.focus();
        } catch {}
      }, 30);
      return () => clearTimeout(timer);
    }
  }, [visible, focused, focusRequest, id]);

  return (
    <div
      ref={ref}
      onFocusCapture={() => onFocusRef.current?.()}
      className="h-full w-full"
      // Small inset so the terminal text isn't glued to the pane edge. FitAddon
      // accounts for padding when computing cols/rows.
      style={{ display: visible ? "block" : "none", padding: "4px 8px" }}
    />
  );
}
