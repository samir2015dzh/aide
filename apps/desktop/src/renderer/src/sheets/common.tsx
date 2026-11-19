import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Mark } from "@hermesh/ui";
import { errorText } from "../store";

/** An action a sheet waits on; its error stays in the sheet. Resolves to whether it worked. */
export function useAction() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const go = async (action: () => Promise<unknown>) => {
    setBusy(true);
    setError("");
    try {
      await action();
      return true;
    } catch (e) {
      setError(errorText(e));
      return false;
    } finally {
      setBusy(false);
    }
  };
  return { busy, error, go };
}

/** While an action the sheet started is under way. */
export function Working() {
  const { t } = useTranslation("sheets");
  return (
    <div className="waitline">
      <Mark size={24} variant="seq" />
      <div>
        <b>{t("working")}</b>
      </div>
    </div>
  );
}

export function ErrorLine({ text }: { text: string }) {
  return text ? <p className="sheet-err" role="alert">{text}</p> : null;
}

/** The host of a custodian URL ("https://example.com" → "example.com"). */
export function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}
