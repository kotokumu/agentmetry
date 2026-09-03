import { afterEach, describe, expect, it, vi } from "vitest";
import "./investigation-filter";
import type { InvestigationFilter } from "./investigation-filter";
afterEach(() => document.body.replaceChildren());
describe("investigation filter draft", () => {
  it("keeps applied conditions visible while rejecting invalid drafts", async () => {
    const panel = document.createElement("am-investigation-filter") as InvestigationFilter;
    panel.filters = { range: "24h", sourceId: "codex", search: "", tool: "Read" };
    document.body.append(panel); await panel.updateComplete;
    const submitted = vi.fn(); panel.addEventListener("investigation-filters-requested", submitted);
    panel.shadowRoot!.querySelector<HTMLInputElement>("[name=minDurationMs]")!.value = "20";
    panel.shadowRoot!.querySelector<HTMLInputElement>("[name=maxDurationMs]")!.value = "10";
    panel.shadowRoot!.querySelector("form")!.dispatchEvent(new Event("submit", { cancelable: true })); await panel.updateComplete;
    expect(submitted).not.toHaveBeenCalled(); expect(panel.shadowRoot!.querySelector("[role=alert]")?.textContent).toContain("Minimum");
    expect(panel.shadowRoot!.querySelector(".applied")?.textContent).toContain("Read");
    panel.shadowRoot!.querySelector<HTMLInputElement>("[name=maxDurationMs]")!.value = "30";
    panel.shadowRoot!.querySelector("form")!.dispatchEvent(new Event("submit", { cancelable: true })); await panel.updateComplete;
    expect(submitted.mock.calls[0]?.[0].detail.filters).toMatchObject({ minDurationMs: 20, maxDurationMs: 30, tool: "Read", sourceId: "codex", range: "24h" });
    expect(panel.shadowRoot!.querySelector(".applied")?.textContent).not.toContain("20 ms");
  });
});
