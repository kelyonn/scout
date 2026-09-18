"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";

// A minimal toast layer — no dependency, one polite live region. Errors
// from mutations used to vanish silently (the button re-enabled and
// nothing else happened); now every failed Save/Dismiss/Apply/Move
// says so, and the two actions a stray tap most often hits (dismiss,
// apply) confirm with an undo.

export type ToastKind = "info" | "success" | "error";

export interface ToastOptions {
  kind?: ToastKind;
  /** Optional single action, e.g. Undo. */
  action?: { label: string; onClick: () => void };
  /** Auto-dismiss after this many ms. Errors default longer. */
  durationMs?: number;
}

interface ToastItem extends ToastOptions {
  id: number;
  message: string;
  kind: ToastKind;
}

interface ToastApi {
  toast: (message: string, opts?: ToastOptions) => void;
  dismiss: (id: number) => void;
}

const ToastContext = createContext<ToastApi | null>(null);

export function useToast(): ToastApi {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast must be used inside <ToastProvider>");
  return ctx;
}

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const nextId = useRef(1);

  const dismiss = useCallback((id: number) => {
    setItems((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const toast = useCallback((message: string, opts: ToastOptions = {}) => {
    const id = nextId.current++;
    const kind = opts.kind ?? "info";
    setItems((prev) => [...prev.slice(-3), { ...opts, id, message, kind }]);
  }, []);

  const api = useMemo(() => ({ toast, dismiss }), [toast, dismiss]);

  return (
    <ToastContext.Provider value={api}>
      {children}
      <ToastViewport items={items} onDismiss={dismiss} />
    </ToastContext.Provider>
  );
}

function ToastViewport({ items, onDismiss }: { items: ToastItem[]; onDismiss: (id: number) => void }) {
  return (
    <div
      aria-live="polite"
      aria-atomic="false"
      className="pointer-events-none fixed inset-x-0 bottom-[calc(var(--bottom-nav-height)+var(--space-3)+env(safe-area-inset-bottom))] md:bottom-6 md:inset-x-auto md:right-6 z-[1000] flex flex-col items-center md:items-end gap-2 px-4 md:px-0"
    >
      {items.map((t) => (
        <ToastCard key={t.id} item={t} onDismiss={() => onDismiss(t.id)} />
      ))}
    </div>
  );
}

const KIND_STYLES: Record<ToastKind, string> = {
  info: "border-border-strong",
  success: "border-success/40",
  error: "border-danger/40",
};

function ToastCard({ item, onDismiss }: { item: ToastItem; onDismiss: () => void }) {
  const duration = item.durationMs ?? (item.kind === "error" ? 7000 : 4000);

  useEffect(() => {
    const handle = window.setTimeout(onDismiss, duration);
    return () => window.clearTimeout(handle);
  }, [duration, onDismiss]);

  return (
    <div
      role={item.kind === "error" ? "alert" : "status"}
      className={`pointer-events-auto flex w-full max-w-sm items-center gap-3 rounded-[var(--radius-card)] border bg-bg-elevated px-4 py-3 shadow-lg shadow-black/30 ${KIND_STYLES[item.kind]}`}
    >
      <span
        aria-hidden
        className={`h-1.5 w-1.5 shrink-0 rounded-full ${
          item.kind === "error" ? "bg-danger" : item.kind === "success" ? "bg-success" : "bg-text-muted"
        }`}
      />
      <p className="text-label flex-1 text-text-primary">{item.message}</p>
      {item.action && (
        <button
          type="button"
          onClick={() => {
            item.action?.onClick();
            onDismiss();
          }}
          className="text-label shrink-0 rounded-[var(--radius-input)] px-2 py-1 text-accent-primary transition-colors duration-100 hover:bg-accent-subtle"
        >
          {item.action.label}
        </button>
      )}
      <button
        type="button"
        onClick={onDismiss}
        aria-label="Dismiss notification"
        className="shrink-0 rounded-[var(--radius-input)] p-1 text-text-muted transition-colors duration-100 hover:bg-bg-surface hover:text-text-primary"
      >
        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
          <path d="M2.5 2.5l7 7M9.5 2.5l-7 7" />
        </svg>
      </button>
    </div>
  );
}
