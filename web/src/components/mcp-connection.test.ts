import { afterEach, describe, expect, it, vi } from "vitest";
import { mcpEndpointFromOrigin, type MCPConnection } from "./mcp-connection";

const clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, "clipboard");

afterEach(() => {
  document.body.replaceChildren();
  if (clipboardDescriptor) Object.defineProperty(navigator, "clipboard", clipboardDescriptor);
  else delete (navigator as { clipboard?: Clipboard }).clipboard;
});

describe("MCP connection information", () => {
  it("derives the MCP endpoint from the dashboard origin", () => {
    expect(mcpEndpointFromOrigin("http://127.0.0.1:17890")).toBe("http://127.0.0.1:17890/mcp");
    expect(mcpEndpointFromOrigin("https://agentmetry.example/"))
      .toBe("https://agentmetry.example/mcp");
  });

  it("shows the connection contract and recommended first tool", async () => {
    const control = document.createElement("am-mcp-connection") as MCPConnection;
    control.endpoint = "http://127.0.0.1:17890/mcp";
    document.body.append(control);

    await control.updateComplete;
    const disclosure = control.shadowRoot?.querySelector<HTMLButtonElement>("button.disclosure");
    const panel = control.shadowRoot?.querySelector<HTMLElement>(".panel");
    expect(disclosure).not.toBeNull();
    expect(disclosure?.getAttribute("aria-expanded")).toBe("false");
    expect(panel?.hidden).toBe(true);
    disclosure!.click();
    await control.updateComplete;

    expect(disclosure?.getAttribute("aria-expanded")).toBe("true");
    expect(panel?.hidden).toBe(false);
    const content = panel?.textContent ?? "";
    expect(control.shadowRoot?.querySelector<HTMLInputElement>(".endpoint-value")?.value)
      .toBe("http://127.0.0.1:17890/mcp");
    expect(content).toContain("Streamable HTTP");
    expect(content).toContain("No authentication");
    expect(content).toContain("Read only");
    expect(content).toContain("get_agent_context");

    disclosure!.click();
    await control.updateComplete;
    expect(disclosure?.getAttribute("aria-expanded")).toBe("false");
    expect(panel?.hidden).toBe(true);
  });

  it("copies the displayed endpoint and confirms success", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    const control = document.createElement("am-mcp-connection") as MCPConnection;
    control.endpoint = "http://127.0.0.1:17890/mcp";
    document.body.append(control);

    await control.updateComplete;
    const disclosure = control.shadowRoot?.querySelector<HTMLButtonElement>("[aria-controls='mcp-connection-panel']");
    disclosure!.click();
    await control.updateComplete;
    control.shadowRoot?.querySelector<HTMLButtonElement>("button.copy")!.click();

    await vi.waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("http://127.0.0.1:17890/mcp");
      expect(control.shadowRoot?.querySelector("[aria-live='polite']")?.textContent).toBe("Copied to clipboard");
    });
    disclosure!.click();
    await control.updateComplete;
    disclosure!.click();
    await control.updateComplete;
    expect(control.shadowRoot?.querySelector<HTMLButtonElement>("button.copy")?.textContent).toBe("Copy URL");
    expect(control.shadowRoot?.querySelector("[aria-live='polite']")?.textContent).toBe("");
  });

  it("reports a copy failure without hiding the endpoint", async () => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: vi.fn().mockRejectedValue(new Error("denied")) },
    });
    const control = document.createElement("am-mcp-connection") as MCPConnection;
    control.endpoint = "http://127.0.0.1:17890/mcp";
    document.body.append(control);

    await control.updateComplete;
    control.shadowRoot?.querySelector<HTMLButtonElement>("button.disclosure")!.click();
    await control.updateComplete;
    control.shadowRoot?.querySelector<HTMLButtonElement>("button.copy")!.click();

    await vi.waitFor(() => expect(control.shadowRoot?.querySelector("[aria-live='polite']")?.textContent)
      .toBe("Copy failed — select the URL manually"));
    expect(control.shadowRoot?.querySelector<HTMLInputElement>(".endpoint-value")?.value)
      .toBe("http://127.0.0.1:17890/mcp");
  });

  it("keeps a keyboard-selectable fallback when the Clipboard API is unavailable", async () => {
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
    const control = document.createElement("am-mcp-connection") as MCPConnection;
    control.endpoint = "http://127.0.0.1:17890/mcp";
    document.body.append(control);

    await control.updateComplete;
    control.shadowRoot?.querySelector<HTMLButtonElement>("button.disclosure")!.click();
    await control.updateComplete;
    control.shadowRoot?.querySelector<HTMLButtonElement>("button.copy")!.click();

    await vi.waitFor(() => expect(control.shadowRoot?.querySelector("[aria-live='polite']")?.textContent)
      .toBe("Copy failed — select the URL manually"));
    const endpoint = control.shadowRoot?.querySelector<HTMLInputElement>(".endpoint-value");
    expect(endpoint?.readOnly).toBe(true);
    expect(endpoint?.getAttribute("aria-label")).toBe("MCP server URL");
    expect(endpoint?.value).toBe("http://127.0.0.1:17890/mcp");
  });

  it("dismisses with Escape and ignores a stale copy result", async () => {
    let finishCopy: (() => void) | undefined;
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: vi.fn(() => new Promise<void>((resolve) => { finishCopy = resolve; })) },
    });
    const control = document.createElement("am-mcp-connection") as MCPConnection;
    document.body.append(control);

    await control.updateComplete;
    const disclosure = control.shadowRoot?.querySelector<HTMLButtonElement>("button.disclosure");
    disclosure!.click();
    await control.updateComplete;
    control.shadowRoot?.querySelector<HTMLButtonElement>("button.copy")!.click();
    control.shadowRoot?.querySelector<HTMLElement>(".panel")!.dispatchEvent(new KeyboardEvent("keydown", {
      key: "Escape",
      bubbles: true,
      composed: true,
    }));
    await control.updateComplete;

    expect(disclosure?.getAttribute("aria-expanded")).toBe("false");
    expect(control.shadowRoot?.activeElement).toBe(disclosure);
    finishCopy?.();
    await Promise.resolve();
    await control.updateComplete;
    expect(control.shadowRoot?.querySelector(".copy-status")?.textContent).toBe("");
  });
});
