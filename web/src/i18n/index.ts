import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import LanguageDetector from "i18next-browser-languagedetector";

// Locale files are auto-discovered so adding a page namespace only means
// dropping `locales/<lang>/<ns>.ts` — no central registration needed.
const enMods = import.meta.glob("./locales/en/*.ts", { eager: true }) as Record<
  string,
  { default: Record<string, unknown> }
>;
const zhMods = import.meta.glob("./locales/zh/*.ts", { eager: true }) as Record<
  string,
  { default: Record<string, unknown> }
>;

function toResources(mods: Record<string, { default: Record<string, unknown> }>) {
  const res: Record<string, Record<string, unknown>> = {};
  for (const path of Object.keys(mods)) {
    const ns = path.split("/").pop()!.replace(/\.ts$/, "");
    res[ns] = mods[path].default;
  }
  return res;
}

const en = toResources(enMods);
const zh = toResources(zhMods);

export const LANGUAGES = [
  { code: "en", label: "English" },
  { code: "zh", label: "简体中文" },
] as const;

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: { en, zh },
    fallbackLng: "en",
    supportedLngs: ["en", "zh"],
    load: "languageOnly",
    nonExplicitSupportedLngs: true,
    defaultNS: "common",
    ns: Object.keys(en),
    interpolation: { escapeValue: false },
    detection: {
      order: ["localStorage", "navigator"],
      caches: ["localStorage"],
      lookupLocalStorage: "gerrit-go-lang",
    },
  });

// Keep <html lang> in sync so screen readers and hyphenation match the UI.
const applyLang = (lng: string) => {
  document.documentElement.lang = lng;
};
applyLang(i18n.language);
i18n.on("languageChanged", applyLang);

export default i18n;
