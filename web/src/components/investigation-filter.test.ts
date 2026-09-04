import { Storage as BrowserStorage } from "happy-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { loadSavedFilters, saveFilter, SAVED_FILTERS_STORAGE_KEY } from "../model/saved-filters";
import "./investigation-filter";
import type { InvestigationFilter } from "./investigation-filter";

beforeEach(() => vi.stubGlobal("localStorage", new BrowserStorage()));
afterEach(() => {
  document.body.replaceChildren();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
describe("investigation filter draft", () => {
  it("keeps saved and draft filter actions in one clear workflow", async () => {
    const saved = { range: "7d" as const, sourceId: "codex", search: "retry", observedFailure: true };
    saveFilter(window.localStorage, "Daily failures", saved);
    const panel = document.createElement("am-investigation-filter") as InvestigationFilter;
    panel.filters = { range: "24h", sourceId: "", search: "" };
    const requested = vi.fn();
    panel.addEventListener("investigation-filters-requested", requested);
    document.body.append(panel);
    await panel.updateComplete;

    expect(panel.shadowRoot!.querySelector<HTMLDetailsElement>("details.editor")?.open).toBe(false);
    expect(panel.shadowRoot!.querySelector("h3")?.textContent).toBe("Investigation filters");
    expect(panel.shadowRoot!.querySelector("button[data-action='apply-draft']")?.textContent).toBe("Apply filters");
    expect(panel.shadowRoot!.querySelector("button[data-action='save-as']")?.textContent).toBe("Save current filters as…");
    expect(panel.shadowRoot!.querySelector("button[data-action='update-saved']")).toBeNull();
    expect(panel.shadowRoot!.querySelector("button[data-action='delete-saved']")).toBeNull();

    const select = panel.shadowRoot!.querySelector<HTMLSelectElement>("select[name='saved-filter']")!;
    select.value = "Daily failures";
    select.dispatchEvent(new Event("change"));
    await panel.updateComplete;

    expect((requested.mock.calls[0][0] as CustomEvent).detail).toEqual({ filters: saved });
    expect(panel.shadowRoot!.querySelector("[role='status']")).toBeNull();
    expect(panel.shadowRoot!.querySelector("button[data-action='update-saved']")?.textContent).toBe("Update saved filter");
    expect(panel.shadowRoot!.querySelector("button[data-action='delete-saved']")?.textContent).toBe("Delete");
    expect(panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='update-saved']")!.disabled).toBe(true);

    panel.pending = true;
    await panel.updateComplete;
    expect(panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='delete-saved']")!.disabled).toBe(true);
    panel.error = "Query failed";
    panel.pending = false;
    await panel.updateComplete;
    expect(panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='update-saved']")!.disabled).toBe(true);

    panel.error = "";
    select.dispatchEvent(new Event("change"));
    panel.pending = true;
    await panel.updateComplete;
    panel.filters = saved;
    panel.pending = false;
    await panel.updateComplete;
    expect(panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='update-saved']")!.disabled).toBe(false);
  });

  it("saves the confirmed filters through the contextual Save as flow", async () => {
    const panel = document.createElement("am-investigation-filter") as InvestigationFilter;
    panel.filters = { range: "24h", sourceId: "codex", search: "retry", tool: "Read" };
    document.body.append(panel);
    await panel.updateComplete;

    panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='save-as']")!.click();
    await panel.updateComplete;
    const name = panel.shadowRoot!.querySelector<HTMLInputElement>("input[name='filter-name']")!;
    expect(panel.shadowRoot!.activeElement).toBe(name);
    name.value = "Read retries";
    name.dispatchEvent(new Event("input"));
    panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='save']")!.click();
    await panel.updateComplete;

    expect(loadSavedFilters(window.localStorage)).toEqual([{ name: "Read retries", filters: panel.filters }]);
    expect(panel.shadowRoot!.querySelector("[role='status']")?.textContent).toContain("Saved");
    expect(panel.shadowRoot!.querySelector("input[name='filter-name']")).toBeNull();
    expect(panel.shadowRoot!.activeElement).toBe(panel.shadowRoot!.querySelector("button[data-action='save-as']"));

    panel.filters = { range: "7d", sourceId: "codex", search: "", observedFailure: true };
    await panel.updateComplete;
    panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='update-saved']")!.click();
    await panel.updateComplete;
    expect(loadSavedFilters(window.localStorage)).toEqual([{ name: "Read retries", filters: panel.filters }]);
    panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='delete-saved']")!.click();
    await panel.updateComplete;
    expect(loadSavedFilters(window.localStorage)).toEqual([]);
    expect(panel.shadowRoot!.querySelector("button[data-action='update-saved']")).toBeNull();
    expect(panel.shadowRoot!.activeElement).toBe(panel.shadowRoot!.querySelector("select[name='saved-filter']"));
  });

  it("blocks unconfirmed persistence and repeated requests while a query is pending", async () => {
    const filters = { range: "24h" as const, sourceId: "codex", search: "" };
    saveFilter(window.localStorage, "Existing", filters);
    const panel = document.createElement("am-investigation-filter") as InvestigationFilter;
    panel.filters = filters;
    panel.confirmed = false;
    const requested = vi.fn();
    panel.addEventListener("investigation-filters-requested", requested);
    document.body.append(panel);
    await panel.updateComplete;

    expect(panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='save-as']")!.disabled).toBe(true);
    const select = panel.shadowRoot!.querySelector<HTMLSelectElement>("select[name='saved-filter']")!;
    select.value = "Existing";
    select.dispatchEvent(new Event("change"));
    await panel.updateComplete;
    expect(requested).toHaveBeenCalledOnce();
    expect(panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='update-saved']")!.disabled).toBe(true);

    panel.pending = true;
    await panel.updateComplete;
    expect(select.disabled).toBe(true);
    select.dispatchEvent(new Event("change"));
    expect(requested).toHaveBeenCalledOnce();
  });

  it("reports corrupt or externally removed saved filters without changing active conditions", async () => {
    const unsupported = JSON.stringify({ version: 9, filters: [] });
    window.localStorage.setItem(SAVED_FILTERS_STORAGE_KEY, unsupported);
    const corruptPanel = document.createElement("am-investigation-filter") as InvestigationFilter;
    document.body.append(corruptPanel);
    await corruptPanel.updateComplete;
    expect(corruptPanel.shadowRoot!.querySelector("[role='alert']")?.textContent).toContain("Unsupported saved filter version");
    expect(window.localStorage.getItem(SAVED_FILTERS_STORAGE_KEY)).toBe(unsupported);
    window.localStorage.setItem(SAVED_FILTERS_STORAGE_KEY, JSON.stringify({ version: 1, filters: [] }));
    corruptPanel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='reload-saved']")!.click();
    await corruptPanel.updateComplete;
    expect(corruptPanel.shadowRoot!.querySelector("[role='alert']")).toBeNull();
    expect(corruptPanel.shadowRoot!.activeElement).toBe(corruptPanel.shadowRoot!.querySelector("select[name='saved-filter']"));

    document.body.replaceChildren();
    window.localStorage.clear();
    const filters = { range: "24h" as const, sourceId: "codex", search: "" };
    saveFilter(window.localStorage, "Existing", filters);
    const panel = document.createElement("am-investigation-filter") as InvestigationFilter;
    panel.filters = filters;
    const requested = vi.fn();
    panel.addEventListener("investigation-filters-requested", requested);
    document.body.append(panel);
    await panel.updateComplete;
    const select = panel.shadowRoot!.querySelector<HTMLSelectElement>("select[name='saved-filter']")!;
    window.localStorage.clear();
    select.value = "Existing";
    select.dispatchEvent(new Event("change"));
    await panel.updateComplete;
    expect(panel.shadowRoot!.querySelector("[role='alert']")?.textContent).toContain("no longer exists");
    expect(requested).not.toHaveBeenCalled();
    expect(panel.filters).toEqual(filters);
  });

  it("keeps the naming flow open when local persistence fails", async () => {
    const originalStorage = window.localStorage;
    const panel = document.createElement("am-investigation-filter") as InvestigationFilter;
    panel.filters = { range: "24h", sourceId: "codex", search: "" };
    document.body.append(panel);
    await panel.updateComplete;
    panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='save-as']")!.click();
    await panel.updateComplete;
    const input = panel.shadowRoot!.querySelector<HTMLInputElement>("input[name='filter-name']")!;
    input.value = "New filter";
    input.dispatchEvent(new Event("input"));
    vi.stubGlobal("localStorage", {
      getItem: originalStorage.getItem.bind(originalStorage),
      setItem: () => { throw new Error("Storage unavailable"); },
    });
    panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='save']")!.click();
    await panel.updateComplete;

    expect(panel.shadowRoot!.querySelector("[role='alert']")?.textContent).toContain("Storage unavailable");
    expect(panel.shadowRoot!.querySelector("input[name='filter-name']")).not.toBeNull();
    expect(loadSavedFilters(originalStorage)).toEqual([]);
  });

  it("disables an open naming flow while a query is pending", async () => {
    const panel = document.createElement("am-investigation-filter") as InvestigationFilter;
    panel.filters = { range: "24h", sourceId: "codex", search: "" };
    document.body.append(panel);
    await panel.updateComplete;
    panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='save-as']")!.click();
    await panel.updateComplete;
    panel.pending = true;
    await panel.updateComplete;

    const save = panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='save']")!;
    expect(save.disabled).toBe(true);
    save.click();
    expect(loadSavedFilters(window.localStorage)).toEqual([]);
    expect(panel.shadowRoot!.querySelector("[role='status']")).toBeNull();
    panel.shadowRoot!.querySelector<HTMLButtonElement>(".naming button[type='button']")!.click();
    await panel.updateComplete;
    expect(panel.shadowRoot!.activeElement).toBe(panel.shadowRoot!.querySelector("details.editor > summary"));
  });

  it.each(["update-saved", "delete-saved"])("preserves the selected filter when %s persistence fails", async (action) => {
    const originalStorage = window.localStorage;
    const applied = { range: "24h" as const, sourceId: "codex", search: "retry" };
    const panel = document.createElement("am-investigation-filter") as InvestigationFilter;
    panel.filters = applied;
    document.body.append(panel);
    await panel.updateComplete;
    panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='save-as']")!.click();
    await panel.updateComplete;
    const input = panel.shadowRoot!.querySelector<HTMLInputElement>("input[name='filter-name']")!;
    input.value = "Existing";
    input.dispatchEvent(new Event("input"));
    panel.shadowRoot!.querySelector<HTMLButtonElement>("button[data-action='save']")!.click();
    await panel.updateComplete;
    const before = loadSavedFilters(originalStorage);
    vi.stubGlobal("localStorage", {
      getItem: originalStorage.getItem.bind(originalStorage),
      setItem: () => { throw new Error("Storage unavailable"); },
    });

    panel.shadowRoot!.querySelector<HTMLButtonElement>(`button[data-action='${action}']`)!.click();
    await panel.updateComplete;

    expect(panel.shadowRoot!.querySelector("[role='alert']")?.textContent).toContain("Storage unavailable");
    expect(panel.shadowRoot!.querySelector("[role='status']")).toBeNull();
    expect(loadSavedFilters(originalStorage)).toEqual(before);
    expect(panel.filters).toEqual(applied);
    expect(panel.shadowRoot!.querySelector<HTMLSelectElement>("select[name='saved-filter']")!.value).toBe("Existing");
  });

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
