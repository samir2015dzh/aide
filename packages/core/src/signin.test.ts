import { describe, expect, test } from "vitest";
import { parseAppLink, parseConnectAddress, projectIdFromFolder } from "./signin";

describe("sign-in", () => {
  test("links: a link code, or an owner to join; anything else is not ours", () => {
    expect(parseAppLink("hermesh://link?code=ABCD-EFGH")).toEqual({ kind: "link", code: "ABCD-EFGH" });
    expect(parseAppLink("hermesh://join?owner=o_123")).toEqual({ kind: "join", owner: "o_123" });
    expect(parseAppLink("hermesh://link")).toBeNull();
    expect(parseAppLink("hermesh://join?owner=")).toBeNull();
    expect(parseAppLink("hermesh://wake?code=abc234")).toEqual({ kind: "wake", code: "abc234" });
    expect(parseAppLink("hermesh://wake")).toBeNull();
    expect(parseAppLink("hermesh://other?code=x")).toBeNull();
    expect(parseAppLink("https://link?code=x")).toBeNull();
    expect(parseAppLink("not a url")).toBeNull();
  });

  test("connection addresses", () => {
    const owner = "o_" + "a".repeat(24);
    expect(parseConnectAddress(`relay.example.com/${owner}`)).toEqual({ relay: "https://relay.example.com", owner });
    expect(parseConnectAddress(` http://127.0.0.1:8080/${owner}/ `)).toEqual({ relay: "http://127.0.0.1:8080", owner });
    expect(parseConnectAddress(`example.com/hermesh/${owner}`)).toEqual({ relay: "https://example.com/hermesh", owner });
    expect(parseConnectAddress("relay.example.com")).toBeNull();
    expect(parseConnectAddress("relay.example.com/o_short")).toBeNull();
    expect(parseConnectAddress("")).toBeNull();
  });

  test("project ids from folder names", () => {
    expect(projectIdFromFolder("My App", new Set())).toBe("my-app");
    expect(projectIdFromFolder(".dotfiles", new Set())).toBe("dotfiles");
    expect(projectIdFromFolder("笔记", new Set())).toBe("project");
    expect(projectIdFromFolder("app", new Set(["app", "app-2"]))).toBe("app-3");
  });
});
