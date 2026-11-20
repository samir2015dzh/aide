import { pickLanguage } from "@aide/core";
import { languages, setLanguage, useWindowsWords, type Language } from "../../shared/i18n";
import { platform } from "./platform";

// The page's language: the one the main process chose for this run, or (a
// page opened outside the app) the browser's. Imported first by main.tsx,
// before any module that shows text.
const chosen = window.aide?.language;
const lng: Language = chosen && languages.includes(chosen) ? chosen : pickLanguage(navigator.languages);
setLanguage(lng);
if (platform === "win") useWindowsWords();
document.documentElement.lang = lng;
