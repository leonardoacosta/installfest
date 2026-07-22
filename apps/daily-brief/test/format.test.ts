/**
 * format.test.ts — `stripControlChars` unit suite
 * (harden-daily-brief-titles-and-tests task 2.1).
 */
import { describe, expect, test } from "bun:test";
import { stripControlChars } from "../src/ui/format";

describe("stripControlChars", () => {
  test("empty string returns empty string", () => {
    expect(stripControlChars("")).toBe("");
  });

  test("plain string with no control characters is unchanged", () => {
    expect(stripControlChars("hello world")).toBe("hello world");
  });

  test("ANSI escape sequence has its ESC byte stripped", () => {
    expect(stripControlChars("a\x1b[31mb")).toBe("a[31mb");
  });

  test("C1 control character is stripped", () => {
    expect(stripControlChars("a\x9bb")).toBe("ab");
  });

  test("tab is stripped", () => {
    expect(stripControlChars("a\tb")).toBe("ab");
  });
});
