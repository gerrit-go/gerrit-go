import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Save, Trash2 } from "lucide-react";
import {
  api,
  SUBMIT_TYPES,
  type ProjectConfig,
  type SubmitRequirement,
} from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const selectCls =
  "h-9 rounded-md border border-input bg-transparent px-2 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50";

export default function ProjectSubmitPanel({ project }: { project: string }) {
  const { t } = useTranslation("projectSubmit");
  const [config, setConfig] = useState<ProjectConfig | null>(null);
  const [canEdit, setCanEdit] = useState(false);
  const [submitType, setSubmitType] = useState("REBASE_IF_NECESSARY");
  const [wholeTopic, setWholeTopic] = useState(false);
  const [reqs, setReqs] = useState<SubmitRequirement[]>([]);
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    setConfig(null);
    setDirty(false);
    setError("");
    api
      .projectConfig(project)
      .then((c) => {
        setConfig(c);
        setSubmitType(c.submit_type || "REBASE_IF_NECESSARY");
        setWholeTopic(!!c.submit_whole_topic);
        setReqs((c.submit_requirements ?? []).map((r) => ({ ...r })));
      })
      .catch((err) => setError((err as Error).message));
    api
      .projectAccess(project)
      .then((a) => setCanEdit(!!a.can_edit))
      .catch(() => setCanEdit(false));
  }, [project]);

  const updateReq = (i: number, patch: Partial<SubmitRequirement>) => {
    setReqs((rs) => rs.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
    setDirty(true);
    setSaved(false);
  };
  const addReq = () => {
    setReqs((rs) => [...rs, { label: "", min_value: 2, block_value: -2 }]);
    setDirty(true);
    setSaved(false);
  };
  const removeReq = (i: number) => {
    setReqs((rs) => rs.filter((_, idx) => idx !== i));
    setDirty(true);
    setSaved(false);
  };

  const save = async () => {
    setBusy(true);
    setError("");
    setSaved(false);
    try {
      const cleaned = reqs
        .filter((r) => r.label.trim() !== "")
        .map((r) => ({
          label: r.label.trim(),
          min_value: Number(r.min_value),
          block_value: Number(r.block_value),
        }));
      const updated = await api.setProjectConfig(project, {
        submit_type: submitType,
        submit_whole_topic: wholeTopic,
        submit_requirements: cleaned,
      });
      setConfig((prev) => ({ ...(prev ?? { name: project }), ...updated }));
      setReqs((updated.submit_requirements ?? []).map((r) => ({ ...r })));
      setDirty(false);
      setSaved(true);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  if (!config) {
    return (
      <div className="flex flex-col gap-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full" />
        ))}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      {error && <p className="text-sm text-destructive">{error}</p>}

      <section className="flex max-w-xl flex-col gap-4">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold">{t("submitBehaviour")}</h2>
          {canEdit && (
            <Button size="sm" className="ml-auto" onClick={save} disabled={!dirty || busy}>
              <Save className="size-4" />
              {busy ? t("common:action.saving") : t("common:action.save")}
            </Button>
          )}
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="submit-type">{t("submitType")}</Label>
          {canEdit ? (
            <select
              id="submit-type"
              className={selectCls}
              value={submitType}
              onChange={(e) => {
                setSubmitType(e.target.value);
                setDirty(true);
                setSaved(false);
              }}
            >
              {SUBMIT_TYPES.map((st) => (
                <option key={st} value={st}>
                  {st}
                </option>
              ))}
            </select>
          ) : (
            <span className="text-sm">{submitType}</span>
          )}
          <p className="text-xs text-muted-foreground">
            {t(`submitTypeHelp.${submitType}`, { defaultValue: "" })}
          </p>
        </div>

        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            className="size-4 rounded border-input"
            checked={wholeTopic}
            disabled={!canEdit}
            onChange={(e) => {
              setWholeTopic(e.target.checked);
              setDirty(true);
              setSaved(false);
            }}
          />
          {t("submitWholeTopic")}
          <span className="text-xs text-muted-foreground">{t("wholeTopicHint")}</span>
        </label>

        {saved && <p className="text-sm text-emerald-600">{t("saved")}</p>}
      </section>

      <section className="flex flex-col gap-2">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold">{t("submitRequirements")}</h2>
          {canEdit && (
            <Button size="sm" variant="outline" className="ml-auto" onClick={addReq}>
              <Plus className="size-4" />
              {t("addRequirement")}
            </Button>
          )}
        </div>
        <p className="text-xs text-muted-foreground">{t("requirementsHint")}</p>
        {reqs.length === 0 ? (
          <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
            {t("noRequirements")}
          </p>
        ) : (
          <div className="rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("label")}</TableHead>
                  <TableHead className="w-32">{t("minValue")}</TableHead>
                  <TableHead className="w-32">{t("blockValue")}</TableHead>
                  {canEdit && <TableHead className="w-12" />}
                </TableRow>
              </TableHeader>
              <TableBody>
                {reqs.map((r, i) => (
                  <TableRow key={i}>
                    <TableCell>
                      {canEdit ? (
                        <Input
                          className="h-8 text-xs"
                          value={r.label}
                          placeholder="Code-Review"
                          onChange={(e) => updateReq(i, { label: e.target.value })}
                        />
                      ) : (
                        <span className="text-xs">{r.label}</span>
                      )}
                    </TableCell>
                    <TableCell>
                      {canEdit ? (
                        <Input
                          type="number"
                          className="h-8 w-20 text-xs"
                          value={r.min_value}
                          onChange={(e) => updateReq(i, { min_value: Number(e.target.value) })}
                        />
                      ) : (
                        <span className="font-mono text-xs">{r.min_value}</span>
                      )}
                    </TableCell>
                    <TableCell>
                      {canEdit ? (
                        <Input
                          type="number"
                          className="h-8 w-20 text-xs"
                          value={r.block_value}
                          onChange={(e) => updateReq(i, { block_value: Number(e.target.value) })}
                        />
                      ) : (
                        <span className="font-mono text-xs">{r.block_value}</span>
                      )}
                    </TableCell>
                    {canEdit && (
                      <TableCell>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-8"
                          onClick={() => removeReq(i)}
                          aria-label={t("removeRequirement")}
                        >
                          <Trash2 className="size-4 text-destructive" />
                        </Button>
                      </TableCell>
                    )}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </section>
    </div>
  );
}
