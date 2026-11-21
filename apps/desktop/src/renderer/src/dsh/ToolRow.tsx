import { useTranslation } from "react-i18next";
import type { ToolCall, ToolResult } from "@aide/proto/session";
import { toolTitle } from "../toolTitle";

/**
 * A tool call in a session read from DSH's or Claude Desktop's records: one line, the tool's name and what
 * the call does (its description, or a natural summary: toolTitle), its raw arguments and its result
 * folded under it until opened. Long arguments scroll inside the fold, never across the page.
 */
export function ToolRow({ call, result }: { call: ToolCall; result?: ToolResult }) {
  const { t } = useTranslation("common");
  const title = toolTitle(call);
  // The arguments are worth a fold only when the title is not all they say.
  const args = call.summary.trim();
  return (
    <div className="dsh-msg tool">
      <details>
        <summary title={title || undefined}>
          <span className="tool-name">{call.name}</span>
          {title && <span className="tool-title">{title}</span>}
          {result?.isError && <span className="tool-err">{t("dsh.toolFailed")}</span>}
        </summary>
        <div className="tool-body">
          {args && args !== title && (
            <>
              <div className="tool-h">{t("dsh.toolArgs")}</div>
              <pre>{args}</pre>
            </>
          )}
          {result && (
            <>
              <div className="tool-h">{t("dsh.toolResult")}</div>
              <pre className={result.isError ? "err" : undefined}>{result.summary}</pre>
            </>
          )}
        </div>
      </details>
    </div>
  );
}
