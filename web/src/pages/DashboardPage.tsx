import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Inbox, Send, Star, Archive, Eye } from "lucide-react";
import { api, type ChangeInfo } from "@/lib/api";
import { useAuth } from "@/auth";
import { Card, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { timeAgo } from "@/lib/utils";
import { StatusBadge, VoteChips } from "@/pages/ChangesPage";

function daysAgo(n: number): string {
  return new Date(Date.now() - n * 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
}

type Section = {
  key: string;
  title: string;
  icon: typeof Inbox;
  query: string;
};

function useSections(): Section[] {
  const { t } = useTranslation("dashboard");
  return [
    { key: "incoming", title: t("incoming"), icon: Inbox, query: "reviewer:self -owner:self status:open" },
    { key: "outgoing", title: t("outgoing"), icon: Send, query: "owner:self status:open" },
    { key: "starred", title: t("starred"), icon: Star, query: "is:starred" },
    { key: "watched", title: t("watched"), icon: Eye, query: "is:watched status:open" },
    { key: "closed", title: t("closed"), icon: Archive, query: `status:closed after:${daysAgo(7)}` },
  ];
}

export default function DashboardPage() {
  const { user, loading } = useAuth();
  const { t } = useTranslation("dashboard");
  const navigate = useNavigate();
  const sections = useSections();

  useEffect(() => {
    if (!loading && !user) navigate("/login", { replace: true });
  }, [loading, user, navigate]);

  if (loading || !user) return null;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <h1 className="text-xl font-semibold">{t("common:nav.dashboard")}</h1>
        <span className="text-sm text-muted-foreground">{t("welcome", { name: user.name })}</span>
      </div>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {sections.map((s) => (
          <DashboardSection key={s.key} section={s} />
        ))}
      </div>
    </div>
  );
}

function DashboardSection({ section }: { section: Section }) {
  const { t } = useTranslation("dashboard");
  const [changes, setChanges] = useState<ChangeInfo[] | null>(null);
  const [total, setTotal] = useState(0);
  const Icon = section.icon;

  useEffect(() => {
    let alive = true;
    setChanges(null);
    api
      .listChangesPaged(section.query, 8, 0)
      .then(({ items, total }) => {
        if (!alive) return;
        setChanges(items ?? []);
        setTotal(total);
      })
      .catch(() => {
        if (alive) setChanges([]);
      });
    return () => {
      alive = false;
    };
  }, [section.query]);

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <CardTitle className="flex items-center gap-2 text-base">
          <Icon className="size-4 text-muted-foreground" />
          {section.title}
        </CardTitle>
        <Link
          to={`/?q=${encodeURIComponent(section.query)}`}
          className="text-xs text-muted-foreground hover:underline"
        >
          {changes === null ? "" : total > 0 ? t("viewAllCount", { total }) : t("viewAll")}
        </Link>
      </CardHeader>
      <div className="px-6 pb-4">
        {changes === null ? (
          <div className="flex flex-col gap-2">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-9 w-full" />
            ))}
          </div>
        ) : changes.length === 0 ? (
          <p className="py-4 text-center text-sm text-muted-foreground">{t("empty")}</p>
        ) : (
          <ul className="divide-y">
            {changes.map((c) => (
              <li key={c._number} className="flex items-center gap-3 py-2">
                <span className="w-12 shrink-0 font-mono text-xs text-muted-foreground">{c._number}</span>
                <div className="min-w-0 flex-1">
                  <Link to={`/c/${c._number}`} className="block truncate text-sm font-medium hover:underline">
                    {c.subject}
                  </Link>
                  <div className="truncate text-xs text-muted-foreground">
                    {c.project} : <span className="font-mono">{c.branch}</span> · {c.owner.name} · {timeAgo(c.updated)}
                  </div>
                </div>
                <div className="hidden w-32 shrink-0 sm:block">
                  <VoteChips labels={c.labels} />
                </div>
                <div className="w-20 shrink-0 text-right">
                  <StatusBadge status={c.status} />
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Card>
  );
}
