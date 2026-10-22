import { ReadClipboardText, WriteClipboardText } from "../../../wailsjs/go/main/App";

// Prefer the desktop bridge: browser clipboard access depends on WebView focus
// and user activation. The fallback also lets the frontend run in a browser.
export async function readTerminalClipboard(): Promise<string> {
  try {
    return await ReadClipboardText();
  } catch (error) {
    if (!navigator.clipboard?.readText) throw error;
    return navigator.clipboard.readText();
  }
}

export async function writeTerminalClipboard(text: string): Promise<void> {
  try {
    await WriteClipboardText(text);
  } catch (error) {
    if (!navigator.clipboard?.writeText) throw error;
    await navigator.clipboard.writeText(text);
  }
}
