import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { Bell, CheckCheck } from "lucide-react";
import { api, type NotificationInfo } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";

function timeAgo(iso: string, t: TFunction): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "";
  const secs = Math.floor((Date.now() - then) / 1000);
  if (secs < 60) return t("justNow");
  const mins = Math.floor(secs / 60);
  if (mins < 60) return t("minutesAgo", { n: mins });
  const hours = Math.floor(mins / 60);
  if (hours < 24) return t("hoursAgo", { n: hours });
  const days = Math.floor(hours / 24);
  return t("daysAgo", { n: days });
}

export default function NotificationBell() {
  const { t } = useTranslation("notifications");
  const navigate = useNavigate();
  const [items, setItems] = useState<NotificationInfo[]>([]);
  const [unread, setUnread] = useState(0);
  const [open, setOpen] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await api.listNotifications(30);
      setItems(res.notifications ?? []);
      setUnread(res.unread ?? 0);
    } catch {
      /* ignore */
    }
  }, []);

  useEffect(() => {
    load();
    const timer = setInterval(load, 20000);
    return () => clearInterval(timer);
  }, [load]);

  const markAll = async () => {
    const res = await api.markNotificationsRead();
    setUnread(res.unread);
    setItems((xs) => xs.map((x) => ({ ...x, read: true })));
  };

  const openItem = async (n: NotificationInfo) => {
    setOpen(false);
    if (!n.read) {
      const res = await api.markNotificationsRead(n.id);
      setUnread(res.unread);
      setItems((xs) => xs.map((x) => (x.id === n.id ? { ...x, read: true } : x)));
    }
    navigate(`/c/${n.change_number}`);
  };

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={t("title")} className="relative">
          <Bell className="size-4" />
          {unread > 0 && (
            <span className="absolute -right-0.5 -top-0.5 flex size-4 items-center justify-center rounded-full bg-destructive text-[10px] font-semibold text-destructive-foreground">
              {unread > 9 ? "9+" : unread}
            </span>
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-80 p-0">
        <DropdownMenuLabel className="flex items-center justify-between">
          <span>{t("title")}</span>
          {unread > 0 && (
            <Button variant="ghost" size="sm" className="h-6 px-2 text-xs" onClick={markAll}>
              <CheckCheck className="size-3.5" />
              {t("markAllRead")}
            </Button>
          )}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {items.length === 0 ? (
          <p className="px-3 py-6 text-center text-sm text-muted-foreground">{t("empty")}</p>
        ) : (
          <div className="max-h-96 overflow-y-auto">
            {items.map((n) => (
              <DropdownMenuItem
                key={n.id}
                onSelect={(e) => {
                  e.preventDefault();
                  openItem(n);
                }}
                className={cn(
                  "flex cursor-pointer flex-col items-start gap-0.5 border-b border-border/50 px-3 py-2 last:border-0",
                  !n.read && "bg-accent/40",
                )}
              >
                <div className="flex w-full items-center gap-2">
                  <Badge variant="secondary" className="text-[10px]">
                    {n.type}
                  </Badge>
                  {!n.read && <span className="size-1.5 rounded-full bg-primary" />}
                  <span className="ml-auto text-[10px] text-muted-foreground">
                    {timeAgo(n.created, t)}
                  </span>
                </div>
                <span className="line-clamp-2 text-xs">{n.message}</span>
                <span className="text-[10px] text-muted-foreground">#{n.change_number}</span>
              </DropdownMenuItem>
            ))}
          </div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
