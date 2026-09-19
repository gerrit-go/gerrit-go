import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";
import { AlertCircle, CheckCircle2, Info, X } from "lucide-react";

type ToastKind = "success" | "error" | "info";

interface ToastItem {
  id: number;
  kind: ToastKind;
  message: string;
}

type PushFn = (message: string, kind?: ToastKind) => void;

const ToastCtx = createContext<PushFn | null>(null);

export function useToast(): PushFn {
  return useContext(ToastCtx) ?? (() => {});
}

const ICONS: Record<ToastKind, typeof Info> = {
  success: CheckCircle2,
  error: AlertCircle,
  info: Info,
};

const TINTS: Record<ToastKind, string> = {
  success: "text-emerald-600 dark:text-emerald-400",
  error: "text-destructive",
  info: "text-muted-foreground",
};

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const seq = useRef(0);

  const dismiss = useCallback((id: number) => {
    setItems((cur) => cur.filter((t) => t.id !== id));
  }, []);

  const push = useCallback<PushFn>((message, kind = "info") => {
    const id = ++seq.current;
    setItems((cur) => [...cur.slice(-3), { id, kind, message }]);
    setTimeout(() => dismiss(id), kind === "error" ? 5000 : 3200);
  }, [dismiss]);

  return (
    <ToastCtx.Provider value={push}>
      {children}
      <div className="pointer-events-none fixed right-4 top-4 z-[80] flex w-[min(92vw,22rem)] flex-col gap-2">
        {items.map((t) => {
          const Icon = ICONS[t.kind];
          return (
            <div
              key={t.id}
              role="status"
              className="animate-toast-in pointer-events-auto flex items-start gap-2 rounded-lg border bg-background px-3 py-2 text-sm shadow-lg"
            >
              <Icon className={`mt-0.5 size-4 shrink-0 ${TINTS[t.kind]}`} />
              <span className="min-w-0 flex-1 break-words">{t.message}</span>
              <button
                className="shrink-0 text-muted-foreground transition-colors hover:text-foreground"
                onClick={() => dismiss(t.id)}
                aria-label="close"
              >
                <X className="size-3.5" />
              </button>
            </div>
          );
        })}
      </div>
    </ToastCtx.Provider>
  );
}
