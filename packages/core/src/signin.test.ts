import { describe, expect, test } from "vitest";
import { parseAppLink, projectIdFromFolder } from "./signin";

describe("sign-in", () => {
  test("links: a wake link; anything else is not ours", () => {
    expect(parseAppLink("aide://link?code=ABCD-EFGH")).toBeNull();
    expect(parseAppLink("aide://join?owner=o_123")).toBeNull();
    expect(parseAppLink("aide://wake?code=abc234")).toEqual({ kind: "wake", code: "abc234" });
    expect(parseAppLink("aide://wake")).toBeNull();
    expect(parseAppLink("aide://other?code=x")).toBeNull();
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
