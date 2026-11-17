import i18n from "i18next"
import { initReactI18next } from "react-i18next"
import zh from "./zh.json"
import en from "./en.json"

const saved = localStorage.getItem("ns-language")
const language = saved === "en" || saved === "zh" ? saved : "zh"
const syncDocumentLanguage = (next: string) => {
  document.documentElement.lang = next.startsWith("en") ? "en" : "zh-CN"
}

syncDocumentLanguage(language)

void i18n.use(initReactI18next).init({
  resources: { zh: { translation: zh }, en: { translation: en } },
  lng: language,
  fallbackLng: "zh",
  interpolation: { escapeValue: false },
})
i18n.on("languageChanged", syncDocumentLanguage)

export { i18n }
