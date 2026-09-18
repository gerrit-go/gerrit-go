import { Link, NavLink, Route, Routes, useNavigate } from "react-router-dom";
import { GitPullRequestArrow, FolderGit2, LogOut, Search, Sun, Moon, Monitor, Check, Users, LayoutDashboard, Settings, Languages, Menu, ShieldCheck } from "lucide-react";
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
import RolesPage from "@/pages/RolesPage";
import LoginPage from "@/pages/LoginPage";
import NotificationBell from "@/components/NotificationBell";
import ShortcutsHelp from "@/components/ShortcutsHelp";
import { useHotkey } from "@/lib/hotkey";

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
        <Button variant="ghost" size="icon" aria-label={t("theme.select")} className="hidden md:inline-flex">
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
        <Button variant="ghost" size="icon" aria-label={t("lang.select")} className="hidden md:inline-flex">
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

function MobileNav() {
  const { user } = useAuth();
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const { theme, setTheme } = useTheme();
  const links: { to: string; label: string; icon: typeof Sun }[] = [
    ...(user ? [{ to: "/dashboard", label: t("nav.dashboard"), icon: LayoutDashboard }] : []),
    { to: "/", label: t("nav.changes"), icon: GitPullRequestArrow },
    { to: "/projects", label: t("nav.projects"), icon: FolderGit2 },
    ...(user ? [{ to: "/groups", label: t("nav.groups"), icon: Users }] : []),
    ...(user?.admin ? [{ to: "/roles", label: t("nav.roles"), icon: ShieldCheck }] : []),
  ];
  const themeOptions: { value: Theme; label: string; icon: typeof Sun }[] = [
    { value: "light", label: t("theme.light"), icon: Sun },
    { value: "dark", label: t("theme.dark"), icon: Moon },
    { value: "system", label: t("theme.system"), icon: Monitor },
  ];
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="md:hidden" aria-label={t("nav.menu")}>
          <Menu className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-48">
        {links.map((l) => (
          <DropdownMenuItem key={l.to} onClick={() => navigate(l.to)}>
            <l.icon className="size-4" />
            {l.label}
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuLabel className="text-xs text-muted-foreground">{t("theme.select")}</DropdownMenuLabel>
        {themeOptions.map((o) => (
          <DropdownMenuItem key={o.value} onClick={() => setTheme(o.value)}>
            <o.icon className="size-4" />
            {o.label}
            {theme === o.value && <Check className="ml-auto size-4" />}
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuLabel className="text-xs text-muted-foreground">{t("lang.select")}</DropdownMenuLabel>
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
      <div className="mx-auto flex h-14 max-w-7xl items-center gap-2 px-4 sm:gap-4">
        <Link to="/" className="flex items-center gap-2 font-semibold">
          <span className="flex size-7 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <GitPullRequestArrow className="size-4" />
          </span>
          <span className="hidden sm:inline">{t("brand")}</span>
        </Link>
        <MobileNav />
        <nav className="hidden items-center gap-1 text-sm md:flex">
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
        <div className="ml-auto flex items-center gap-1 md:ml-0">
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
      </div>
      <form onSubmit={onSearch} className="border-t px-4 py-2 md:hidden">
        <div className="relative mx-auto max-w-7xl">
          <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("search.placeholder")}
            className="pl-8"
          />
        </div>
      </form>
    </header>
  );
}

export default function App() {
  const navigate = useNavigate();
  const [helpOpen, setHelpOpen] = useState(false);
  const [pending, setPending] = useState("");

  useHotkey((e) => {
    if (e.key === "?") {
      setHelpOpen((v) => !v);
      return;
    }
    if (e.key === "u") {
      navigate(-1);
      return;
    }
    // Two-key "g X" navigation sequences.
    if (pending === "g") {
      setPending("");
      if (e.key === "c") navigate("/");
      else if (e.key === "d") navigate("/dashboard");
      else if (e.key === "p") navigate("/projects");
      return;
    }
    if (e.key === "g") {
      setPending("g");
      setTimeout(() => setPending(""), 800);
    }
  });

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
          <Route path="/roles" element={<RolesPage />} />
          <Route path="/c/:num" element={<ChangeDetailPage />} />
          <Route path="*" element={<NotFound />} />
        </Routes>
      </main>
      <ShortcutsHelp open={helpOpen} onOpenChange={setHelpOpen} />
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
