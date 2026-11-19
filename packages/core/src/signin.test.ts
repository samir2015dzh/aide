import { describe, expect, test } from "vitest";
import { parseAppLink, projectIdFromFolder } from "./signin";

describe("sign-in", () => {
  test("links: a wake link; anything else is not ours", () => {
    expect(parseAppLink("hermesh://link?code=ABCD-EFGH")).toBeNull();
    expect(parseAppLink("hermesh://join?owner=o_123")).toBeNull();
    expect(parseAppLink("hermesh://wake?code=abc234")).toEqual({ kind: "wake", code: "abc234" });
    expect(parseAppLink("hermesh://wake")).toBeNull();
    expect(parseAppLink("hermesh://other?code=x")).toBeNull();
    expect(parseAppLink("https://link?code=x")).toBeNull();
    expect(parseAppLink("not a url")).toBeNull();
  });

  test("project ids from folder names", () => {
    expect(projectIdFromFolder("My App", new Set())).toBe("my-app");
    expect(projectIdFromFolder(".dotfiles", new Set())).toBe("dotfiles");
    expect(projectIdFromFolder("笔记", new Set())).toBe("project");
    expect(projectIdFromFolder("app", new Set(["app", "app-2"]))).toBe("app-3");
  });
});
