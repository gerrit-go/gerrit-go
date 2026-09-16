import { Link, NavLink, Route, Routes, useNavigate } from "react-router-dom";
import { GitPullRequestArrow, FolderGit2, LogOut, Search, Sun, Moon, Monitor, Check, Users, LayoutDashboard, Settings, Languages } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useAuth } from "@/auth";
import { useTheme, type Theme } from "@/theme";
import { LANGUAGES } from "@/i18n";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import ChangesPage from "@/pages/ChangesPage";
import ChangeDetailPage from "@/pages/ChangeDetailPage";
import DashboardPage from "@/pages/DashboardPage";
import SettingsPage from "@/pages/SettingsPage";
import ProjectsPage from "@/pages/ProjectsPage";
import ProjectDetailPage from "@/pages/ProjectDetailPage";
import GroupsPage from "@/pages/GroupsPage";
import LoginPage from "@/pages/LoginPage";
import NotificationBell from "@/components/NotificationBell";

function ThemeToggle() {
  const { theme, setTheme } = useTheme();
  const { t } = useTranslation();
  const options: { value: Theme; label: string; icon: typeof Sun }[] = [
    { value: "light", label: t("theme.light"), icon: Sun },
    { value: "dark", label: t("theme.dark"), icon: Moon },
    { value: "system", label: t("theme.system"), icon: Monitor },
  ];
  const CurrentIcon = options.find((o) => o.value === theme)!.icon;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={t("theme.select")}>
          <CurrentIcon className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-36">
        {options.map(({ value, label, icon: Icon }) => (
          <DropdownMenuItem key={value} onClick={() => setTheme(value)}>
            <Icon className="size-4" />
            {label}
            {theme === value && <Check className="ml-auto size-4" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function LanguageSwitcher() {
  const { i18n, t } = useTranslation();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={t("lang.select")}>
          <Languages className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-40">
        {LANGUAGES.map((l) => (
          <DropdownMenuItem key={l.code} onClick={() => void i18n.changeLanguage(l.code)}>
            <span className="text-sm">{l.label}</span>
            {i18n.language === l.code && <Check className="ml-auto size-4" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function Header() {
  const { user, signOut, loading } = useAuth();
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [query, setQuery] = useState("");

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "/" && !(e.target instanceof HTMLInputElement) && !(e.target instanceof HTMLTextAreaElement)) {
        e.preventDefault();
        document.getElementById("global-search")?.focus();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  const onSearch = (e: React.FormEvent) => {
    e.preventDefault();
    navigate(`/?q=${encodeURIComponent(query)}`);
  };

  return (
    <header className="sticky top-0 z-40 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="mx-auto flex h-14 max-w-7xl items-center gap-4 px-4">
        <Link to="/" className="flex items-center gap-2 font-semibold">
          <span className="flex size-7 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <GitPullRequestArrow className="size-4" />
          </span>
          <span className="hidden sm:inline">{t("brand")}</span>
        </Link>
        <nav className="flex items-center gap-1 text-sm">
          {user && (
            <NavLink
              to="/dashboard"
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-muted-foreground transition-colors hover:text-foreground hover:bg-accent",
                  isActive && "bg-accent text-foreground font-medium",
                )
              }
            >
              <LayoutDashboard className="size-4" />
              {t("nav.dashboard")}
            </NavLink>
          )}
          <NavLink
            to="/"
            end
            className={({ isActive }) =>
              cn(
                "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-muted-foreground transition-colors hover:text-foreground hover:bg-accent",
                isActive && "bg-accent text-foreground font-medium",
              )
            }
          >
            <GitPullRequestArrow className="size-4" />
            {t("nav.changes")}
          </NavLink>
          <NavLink
            to="/projects"
            className={({ isActive }) =>
              cn(
                "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-muted-foreground transition-colors hover:text-foreground hover:bg-accent",
                isActive && "bg-accent text-foreground font-medium",
              )
            }
          >
            <FolderGit2 className="size-4" />
            {t("nav.projects")}
          </NavLink>
          {user && (
            <NavLink
              to="/groups"
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-muted-foreground transition-colors hover:text-foreground hover:bg-accent",
                  isActive && "bg-accent text-foreground font-medium",
                )
              }
            >
              <Users className="size-4" />
              {t("nav.groups")}
            </NavLink>
          )}
        </nav>
        <form onSubmit={onSearch} className="ml-auto hidden w-full max-w-sm md:block">
          <div className="relative">
            <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              id="global-search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t("search.placeholder")}
              className="pl-8"
            />
          </div>
        </form>
        <LanguageSwitcher />
        <ThemeToggle />
        {!loading && user && <NotificationBell />}
        {loading ? null : user ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" className="rounded-full px-2">
                <span className="flex size-7 items-center justify-center rounded-full bg-secondary text-xs font-semibold">
                  {user.name.slice(0, 1).toUpperCase()}
                </span>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuLabel>
                <div className="text-sm font-medium">{user.name}</div>
                <div className="text-xs font-normal text-muted-foreground">@{user.username}</div>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => navigate("/settings")}>
                <Settings className="size-4" />
                {t("action.settings")}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={async () => {
                  await signOut();
                  navigate("/login");
                }}
              >
                <LogOut className="size-4" />
                {t("action.signOut")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          <Button asChild size="sm">
            <Link to="/login">{t("action.signIn")}</Link>
          </Button>
        )}
      </div>
    </header>
  );
}

export default function App() {
  return (
    <div className="min-h-screen bg-background">
      <Header />
      <main className="mx-auto max-w-7xl px-4 py-6">
        <Routes>
          <Route path="/" element={<ChangesPage />} />
          <Route path="/dashboard" element={<DashboardPage />} />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="/login" element={<LoginPage />} />
          <Route path="/projects" element={<ProjectsPage />} />
          <Route path="/projects/*" element={<ProjectDetailPage />} />
          <Route path="/groups" element={<GroupsPage />} />
          <Route path="/c/:num" element={<ChangeDetailPage />} />
          <Route path="*" element={<NotFound />} />
        </Routes>
      </main>
    </div>
  );
}

function NotFound() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-center justify-center py-24 text-center">
      <p className="text-4xl font-bold">{t("notFound.code")}</p>
      <p className="mt-2 text-muted-foreground">{t("notFound.message")}</p>
      <Button asChild className="mt-4">
        <Link to="/">{t("notFound.back")}</Link>
      </Button>
    </div>
  );
}
