import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

const GROUPS: { titleKey: string; keys: { keys: string; descKey: string }[] }[] = [
  {
    titleKey: "shortcuts.global",
    keys: [
      { keys: "?", descKey: "shortcuts.help" },
      { keys: "g c", descKey: "shortcuts.goChanges" },
      { keys: "g d", descKey: "shortcuts.goDashboard" },
      { keys: "g p", descKey: "shortcuts.goProjects" },
      { keys: "u", descKey: "shortcuts.goUp" },
    ],
  },
  {
    titleKey: "shortcuts.changePage",
    keys: [
      { keys: "] / [", descKey: "shortcuts.nextPrevFile" },
      { keys: "r", descKey: "shortcuts.reply" },
    ],
  },
];

export default function ShortcutsHelp({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const { t } = useTranslation("common");
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("shortcuts.title")}</DialogTitle>
          <DialogDescription>{t("shortcuts.desc")}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          {GROUPS.map((g) => (
            <div key={g.titleKey}>
              <p className="mb-1.5 text-xs font-semibold text-muted-foreground">{t(g.titleKey)}</p>
              <div className="flex flex-col gap-1">
                {g.keys.map((k) => (
                  <div key={k.keys} className="flex items-center gap-3 text-sm">
                    <kbd className="min-w-16 rounded border bg-muted/40 px-1.5 py-0.5 text-center font-mono text-xs">
                      {k.keys}
                    </kbd>
                    <span className="text-muted-foreground">{t(k.descKey)}</span>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
