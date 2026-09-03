import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Storage as BrowserStorage } from "happy-dom";
import { loadSavedFilters, saveFilter, SAVED_FILTERS_STORAGE_KEY } from "../model/saved-filters";
import { SavedFilters } from "./saved-filters";

beforeEach(() => vi.stubGlobal("localStorage", new BrowserStorage()));
afterEach(() => {
  document.body.replaceChildren();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("saved filter controls", () => {
  it("saves only applied conditions and requests a saved relative filter without claiming query success", async () => {
    const panel = new SavedFilters();
    panel.filters = { range: "24h", sourceId: "codex", search: "retry", observedFailure: true, tool: "exec_command" };
    const requested = vi.fn();
    panel.addEventListener("investigation-filters-requested", requested);
    document.body.append(panel);
    await panel.updateComplete;
    const input = panel.shadowRoot?.querySelector<HTMLInputElement>("input[name='filter-name']");
    expect(input).not.toBeNull();
    input!.value = "Daily failures";
    input!.dispatchEvent(new Event("input"));
    panel.shadowRoot?.querySelector<HTMLButtonElement>("button[data-action='save']")?.click();
    await panel.updateComplete;

    expect(loadSavedFilters(window.localStorage)).toEqual([{ name: "Daily failures", filters: panel.filters }]);
    expect(requested).not.toHaveBeenCalled();
    expect(panel.shadowRoot?.textContent).toContain("Save currently applied conditions");
    expect(panel.shadowRoot?.querySelector("[role='status']")?.textContent).toContain("Saved");
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-05T12:00:00Z"));
    panel.shadowRoot?.querySelector<HTMLButtonElement>("button[data-action='apply']")?.click();
    await panel.updateComplete;
    expect((requested.mock.calls[0][0] as CustomEvent).detail).toEqual({ filters: panel.filters });
    expect(panel.shadowRoot?.querySelector("[role='status']")?.textContent ?? "").not.toContain("Applied");
    panel.filters = { range: "7d", sourceId: "claude", search: "" };
    await panel.updateComplete;
    panel.shadowRoot?.querySelector<HTMLButtonElement>("button[data-action='replace']")?.click();
    await panel.updateComplete;
    expect(loadSavedFilters(window.localStorage)).toEqual([{ name: "Daily failures", filters: panel.filters }]);
    panel.shadowRoot?.querySelector<HTMLButtonElement>("button[data-action='delete']")?.click();
    await panel.updateComplete;
    expect(loadSavedFilters(window.localStorage)).toEqual([]);
    expect(requested).toHaveBeenCalledOnce();
  });

  it("blocks saving unconfirmed conditions but permits a saved request after failure", async () => {
    const filters = { range: "24h" as const, sourceId: "codex", search: "" };
    saveFilter(window.localStorage, "Existing", filters);
    const panel = new SavedFilters();
    panel.filters = { ...filters, search: "unconfirmed request" };
    panel.confirmed = false;
    const requested = vi.fn();
    panel.addEventListener("investigation-filters-requested", requested);
    document.body.append(panel);
    await panel.updateComplete;
    const select = panel.shadowRoot?.querySelector<HTMLSelectElement>("select");
    select!.value = "Existing";
    select!.dispatchEvent(new Event("change"));
    await panel.updateComplete;

    expect(panel.shadowRoot?.querySelector<HTMLButtonElement>("button[data-action='save']")?.disabled).toBe(true);
    expect(panel.shadowRoot?.querySelector<HTMLButtonElement>("button[data-action='replace']")?.disabled).toBe(true);
    const apply = panel.shadowRoot?.querySelector<HTMLButtonElement>("button[data-action='apply']");
    expect(apply?.disabled).toBe(false);
    apply?.click();
    expect((requested.mock.calls[0][0] as CustomEvent).detail).toEqual({ filters });
    panel.pending = true;
    await panel.updateComplete;
    expect(apply?.disabled).toBe(true);
    apply?.click();
    expect(requested).toHaveBeenCalledOnce();
    expect(loadSavedFilters(window.localStorage)).toEqual([{ name: "Existing", filters }]);
  });

  it.each(["save", "replace", "delete"])("shows a failed %s without changing the saved collection or active filters", async (action) => {
    const originalStorage = window.localStorage;
    const filters = { range: "24h" as const, sourceId: "codex", search: "" };
    const saved = saveFilter(originalStorage, "Existing", filters);
    const panel = new SavedFilters();
    panel.filters = { ...filters, search: "applied" };
    document.body.append(panel);
    await panel.updateComplete;
    const select = panel.shadowRoot?.querySelector<HTMLSelectElement>("select");
    select!.value = "Existing";
    select!.dispatchEvent(new Event("change"));
    const input = panel.shadowRoot?.querySelector<HTMLInputElement>("input");
    input!.value = "New";
    input!.dispatchEvent(new Event("input"));
    await panel.updateComplete;
    vi.stubGlobal("localStorage", {
      getItem: originalStorage.getItem.bind(originalStorage),
      setItem: () => { throw new Error("Storage unavailable"); },
    });
    panel.shadowRoot?.querySelector<HTMLButtonElement>(`button[data-action='${action}']`)?.click();
    await panel.updateComplete;

    expect(panel.shadowRoot?.querySelector("[role='alert']")?.textContent).toContain("Storage unavailable");
    expect(panel.shadowRoot?.querySelector("[role='status']")).toBeNull();
    expect(loadSavedFilters(originalStorage)).toEqual(saved);
    expect(panel.filters).toEqual({ ...filters, search: "applied" });
    expect(panel.shadowRoot?.querySelector<HTMLSelectElement>("select")?.value).toBe("Existing");
  });

  it("reports unsupported saved data at load and preserves it", async () => {
    const unsupported = JSON.stringify({ version: 9, filters: [] });
    window.localStorage.setItem(SAVED_FILTERS_STORAGE_KEY, unsupported);
    const panel = new SavedFilters();
    document.body.append(panel);
    await panel.updateComplete;
    expect(panel.shadowRoot?.querySelector("[role='alert']")?.textContent).toContain("Unsupported saved filter version");
    expect(window.localStorage.getItem(SAVED_FILTERS_STORAGE_KEY)).toBe(unsupported);
  });

  it("reports a saved filter removed elsewhere instead of silently applying nothing", async () => {
    saveFilter(window.localStorage, "Existing", { range: "24h", sourceId: "codex", search: "" });
    const panel = new SavedFilters();
    const requested = vi.fn();
    panel.addEventListener("investigation-filters-requested", requested);
    document.body.append(panel);
    await panel.updateComplete;
    const select = panel.shadowRoot?.querySelector<HTMLSelectElement>("select");
    select!.value = "Existing";
    select!.dispatchEvent(new Event("change"));
    await panel.updateComplete;
    window.localStorage.clear();
    panel.shadowRoot?.querySelector<HTMLButtonElement>("button[data-action='apply']")?.click();
    await panel.updateComplete;
    expect(panel.shadowRoot?.querySelector("[role='alert']")?.textContent).toContain("no longer exists");
    expect(requested).not.toHaveBeenCalled();
  });
});
