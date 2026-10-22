import { useEffect, useLayoutEffect, useRef, useState, ReactNode } from "react";
import { createPortal } from "react-dom";

type Props = {
  open: boolean;
  anchorRef: React.RefObject<HTMLElement>;
  onClose: () => void;
  align?: "left" | "right"; // edge of menu aligned to the matching edge of the anchor
  children: ReactNode;
  minWidth?: number;
  placement?: "bottom" | "top";
};

type MenuRect = { top: number; bottom: number; left: number; right: number };
type MenuPosition = { top: number; left: number; maxHeight: number };

export function calculatePortalMenuPosition({
  anchor,
  menuWidth,
  menuHeight,
  minWidth,
  viewportWidth,
  viewportHeight,
  align,
  placement,
}: {
  anchor: MenuRect;
  menuWidth: number;
  menuHeight: number;
  minWidth: number;
  viewportWidth: number;
  viewportHeight: number;
  align: "left" | "right";
  placement: "bottom" | "top";
}): MenuPosition {
  const padding = 8;
  const gap = 4;
  const width = Math.max(minWidth, menuWidth);
  const maxHeight = Math.max(0, viewportHeight - padding * 2);
  const height = Math.min(menuHeight, maxHeight);
  const below = anchor.bottom + gap;
  const above = anchor.top - gap - height;

  let top: number;
  if (placement === "top") {
    top = above >= padding ? above : below + height <= viewportHeight - padding ? below : padding;
  } else {
    top = below + height <= viewportHeight - padding ? below : above >= padding ? above : padding;
  }

  const desiredLeft = align === "right" ? anchor.right - width : anchor.left;
  const left = Math.max(padding, Math.min(desiredLeft, viewportWidth - width - padding));
  return { top, left, maxHeight };
}

// Renders a dropdown menu in document.body with fixed positioning relative to
// `anchorRef`. Escapes any ancestor `overflow:hidden|auto` clipping.
export default function PortalMenu({
  open, anchorRef, onClose, align = "right", children, minWidth = 160, placement = "bottom",
}: Props) {
  const [pos, setPos] = useState<MenuPosition | null>(null);
  const [measured, setMeasured] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    if (!open || !anchorRef.current) {
      setPos(null);
      setMeasured(false);
      return;
    }
    const r = anchorRef.current.getBoundingClientRect();
    setMeasured(false);
    setPos(calculatePortalMenuPosition({
      anchor: r,
      menuWidth: minWidth,
      menuHeight: 0,
      minWidth,
      viewportWidth: window.innerWidth,
      viewportHeight: window.innerHeight,
      align,
      placement,
    }));
  }, [open, anchorRef, align, minWidth, placement]);

  useLayoutEffect(() => {
    if (!open || !pos || measured || !anchorRef.current || !menuRef.current) return;
    const anchor = anchorRef.current.getBoundingClientRect();
    const menu = menuRef.current;
    setPos(calculatePortalMenuPosition({
      anchor,
      menuWidth: menu.offsetWidth,
      menuHeight: menu.scrollHeight,
      minWidth,
      viewportWidth: window.innerWidth,
      viewportHeight: window.innerHeight,
      align,
      placement,
    }));
    setMeasured(true);
  }, [open, pos, measured, anchorRef, align, minWidth, placement]);

  // Close on outside click / escape / scroll / resize.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node;
      if (menuRef.current?.contains(t)) return;
      if (anchorRef.current?.contains(t)) return;
      onClose();
    };
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    const onScroll = (event: Event) => {
      const target = event.target;
      if (target instanceof Node && menuRef.current?.contains(target)) return;
      onClose();
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", onScroll);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", onScroll);
    };
  }, [open, onClose, anchorRef]);

  if (!open || !pos) return null;

  return createPortal(
    <div
      ref={menuRef}
      data-portal-menu
      style={{
        position: "fixed",
        top: pos.top,
        left: pos.left,
        minWidth,
        maxWidth: "calc(100vw - 16px)",
        maxHeight: pos.maxHeight,
        overflowY: "auto",
        visibility: measured ? "visible" : "hidden",
        zIndex: 9999,
      }}
      className="glass-strong rounded-lg p-1"
    >
      {children}
    </div>,
    document.body
  );
}
