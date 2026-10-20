"use client";

import { useRef, useState } from "react";
import { lockAxis, swipeAction, swipeOffset, type SwipeAxis } from "@/lib/swipe";
import { canDismiss, canSave, useJobActions } from "@/lib/useJobActions";
import type { ApplicationState } from "@/lib/types";

// docs/12-frontend-ux.md section 6: swipe right = save, left = dismiss,
// both with undo (the toast comes from useJobActions). Touch only — a mouse
// drag on desktop is text selection or a click, never a gesture — and
// `touch-action: pan-y` hands vertical scrolling to the browser, so the
// list scrolls natively and we only ever see the horizontal part. The
// Save/Dismiss buttons stay the accessible path; this is a shortcut.
export function SwipeRow({
  groupId,
  state,
  children,
}: {
  groupId: string;
  state: ApplicationState;
  children: React.ReactNode;
}) {
  const actions = useJobActions();
  const [dx, setDx] = useState(0);
  const [dragging, setDragging] = useState(false);
  const start = useRef<{ x: number; y: number; id: number } | null>(null);
  const axis = useRef<SwipeAxis>("undecided");
  const swiped = useRef(false);

  const saveOk = canSave(state);
  const dismissOk = canDismiss(state);

  function allowed(raw: number): number {
    const v = swipeOffset(raw);
    if (v > 0 && !saveOk) return 0;
    if (v < 0 && !dismissOk) return 0;
    return v;
  }

  function reset() {
    start.current = null;
    axis.current = "undecided";
    setDragging(false);
    setDx(0);
  }

  function onPointerDown(e: React.PointerEvent<HTMLDivElement>) {
    if (e.pointerType !== "touch") return;
    start.current = { x: e.clientX, y: e.clientY, id: e.pointerId };
    axis.current = "undecided";
  }

  function onPointerMove(e: React.PointerEvent<HTMLDivElement>) {
    const s = start.current;
    if (!s || e.pointerId !== s.id) return;
    const rawX = e.clientX - s.x;
    if (axis.current === "undecided") {
      axis.current = lockAxis(rawX, e.clientY - s.y);
      if (axis.current === "horizontal") {
        e.currentTarget.setPointerCapture(e.pointerId);
        setDragging(true);
      }
    }
    if (axis.current === "horizontal") setDx(allowed(rawX));
  }

  function onPointerUp(e: React.PointerEvent<HTMLDivElement>) {
    const s = start.current;
    if (!s || e.pointerId !== s.id) return;
    const wasHorizontal = axis.current === "horizontal";
    const action = wasHorizontal ? swipeAction(allowed(e.clientX - s.x)) : null;
    if (wasHorizontal) {
      // The lift of a drag that began on the stretched title link would
      // otherwise navigate.
      swiped.current = true;
      window.setTimeout(() => (swiped.current = false), 50);
    }
    reset();
    if (action === "save") void actions.save(groupId, state);
    if (action === "dismiss") void actions.dismiss(groupId, state);
  }

  const hint = dx > 0 ? "save" : dx < 0 ? "dismiss" : null;
  const armed = swipeAction(dx) !== null;

  return (
    <div className="relative">
      {hint && (
        <div
          aria-hidden
          className={`absolute inset-0 flex items-center rounded-[var(--radius-card)] px-5 text-label font-semibold ${
            hint === "save" ? "justify-start bg-success/15 text-success" : "justify-end bg-danger/15 text-danger"
          } ${armed ? "opacity-100" : "opacity-60"}`}
        >
          {hint === "save" ? "Save" : "Dismiss"}
        </div>
      )}
      <div
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={reset}
        onClickCapture={(e) => {
          if (swiped.current) {
            e.preventDefault();
            e.stopPropagation();
          }
        }}
        style={{
          touchAction: "pan-y",
          transform: dx === 0 ? undefined : `translateX(${dx}px)`,
          transition: dragging ? "none" : "transform 150ms cubic-bezier(0.2, 0, 0, 1)",
        }}
      >
        {children}
      </div>
    </div>
  );
}
