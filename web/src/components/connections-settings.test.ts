import { afterEach, describe, expect, it, vi } from "vitest";
import "./connections-settings";
import type { ConnectionsSettings } from "./connections-settings";

afterEach(() => {
  document.body.replaceChildren();
  vi.restoreAllMocks();
});

describe("connections settings", () => {
  it("shows source setup, scope limits, inline MCP, and display controls", async () => {
    const settings = document.createElement("am-connections-settings") as ConnectionsSettings;
    document.body.append(settings);
    await settings.updateComplete;
    await Promise.all((Array.from(settings.shadowRoot?.querySelectorAll("*" ) ?? []) as Array<{ updateComplete?: Promise<unknown> }>)
      .map((element) => element.updateComplete ?? Promise.resolve()));
    const content = settings.shadowRoot?.textContent ?? "";
    expect(content).toContain("Claude Code");
    expect(content).toContain("Codex");
    expect(content).toContain("http://127.0.0.1:4317");
    expect(content).toContain("does not edit source files");
    expect(content).toContain("unreported, redacted, or unavailable");
    expect(settings.shadowRoot?.querySelector("am-mcp-connection[inline]")).not.toBeNull();
    expect(settings.shadowRoot?.querySelector("am-language-selector")).not.toBeNull();
    expect(settings.shadowRoot?.querySelector("am-appearance-settings")).not.toBeNull();
    expect(settings.shadowRoot?.querySelector("am-app-update-control")).not.toBeNull();
  });

  it("keeps source setup copy-only when clipboard is unavailable", async () => {
    const settings = document.createElement("am-connections-settings") as ConnectionsSettings;
    document.body.append(settings);
    await settings.updateComplete;
    expect(settings.shadowRoot?.querySelectorAll("input, textarea")).toHaveLength(0);
    expect(settings.shadowRoot?.querySelectorAll("pre")).toHaveLength(2);
  });

  it("copies the README configuration and reports clipboard failure", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    const settings = document.createElement("am-connections-settings") as ConnectionsSettings;
    document.body.append(settings);
    await settings.updateComplete;
    const buttons = settings.shadowRoot?.querySelectorAll<HTMLButtonElement>("button.copy");
    buttons?.[0]?.click();
    await vi.waitFor(() => expect(writeText).toHaveBeenCalledWith(expect.stringContaining('"OTEL_LOG_USER_PROMPTS": "1"')));
    await settings.updateComplete;
    expect(settings.shadowRoot?.querySelector("[aria-live='polite']")?.textContent).toContain("Copied");

    writeText.mockRejectedValueOnce(new Error("denied"));
    buttons?.[1]?.click();
    await vi.waitFor(() => expect(settings.shadowRoot?.querySelector("[aria-live='polite']")?.textContent).toContain("Copy failed"));
  });
});
