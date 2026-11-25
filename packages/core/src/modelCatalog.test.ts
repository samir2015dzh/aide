import { describe, expect, it } from "vitest";
import { create } from "@bufbuild/protobuf";
import { AgentModelSchema } from "@aide/proto/machine";
import { formatContextWindow, modelMetadataSummary } from "./machines";

describe("open model catalog metadata (#114)", () => {
  it("formats context window sizes compactly", () => {
    expect(formatContextWindow(0)).toBe("");
    expect(formatContextWindow(200_000)).toBe("200K");
    expect(formatContextWindow(1_048_576)).toBe("1M");
    expect(formatContextWindow(128_000)).toBe("128K");
  });

  it("summarizes enriched metadata with explicit estimate label and stays empty for unknown models", () => {
    const known = create(AgentModelSchema, {
      id: "sonnet",
      name: "Anthropic · claude-sonnet-4-5",
      resolved: "claude-sonnet-4-5",
      contextWindow: 200_000,
      maxOutputTokens: 64_000,
      capabilities: ["reasoning", "tools", "vision"],
      inputCostPerMillion: 3,
      outputCostPerMillion: 15,
    });
    expect(modelMetadataSummary(known)).toBe("200K · reasoning/tools/vision · ~$3/$15 /1M (est.)");
    expect(modelMetadataSummary(known, "参考估算")).toBe("200K · reasoning/tools/vision · ~$3/$15 /1M (参考估算)");

    const unknown = create(AgentModelSchema, {
      id: "custom-local-1",
      name: "Custom · custom-local-1",
    });
    expect(modelMetadataSummary(unknown)).toBe("");
  });
});
