import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Storage as BrowserStorage } from "happy-dom";
import type { InvestigationFilters } from "./investigation-conditions";
import { deleteFilter, loadSavedFilters, replaceFilter, saveFilter, SAVED_FILTERS_STORAGE_KEY } from "./saved-filters";

beforeEach(() => vi.stubGlobal("localStorage", new BrowserStorage()));

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe("local saved investigation filters", () => {
  it("saves, reloads, replaces and deletes named complete conditions", () => {
    const filters: InvestigationFilters = {
      range: "24h", sourceId: "codex", search: "retry", observedFailure: true,
      minDurationMs: 10, maxDurationMs: 20, model: "model-a", tool: "exec_command",
    };
    const saved = saveFilter(window.localStorage, "Failures", filters);
    expect(saved).toEqual([{ name: "Failures", filters }]);
    expect(loadSavedFilters(window.localStorage)).toEqual(saved);
    const replacement: InvestigationFilters = { range: "7d", sourceId: "claude", search: "" };
    expect(replaceFilter(window.localStorage, "Failures", replacement)).toEqual([{ name: "Failures", filters: replacement }]);
    expect(loadSavedFilters(window.localStorage)).toEqual([{ name: "Failures", filters: replacement }]);
    expect(deleteFilter(window.localStorage, "Failures")).toEqual([]);
    expect(loadSavedFilters(window.localStorage)).toEqual([]);
  });

  it.each([
    ["unknown version", JSON.stringify({ version: 2, filters: [] }), /Unsupported saved filter version/],
    ["unknown stored field", JSON.stringify({ version: 1, filters: [], extra: true }), /Unsupported saved filter field/],
    ["unknown condition", JSON.stringify({ version: 1, filters: [{ name: "Future", filters: { range: "24h", sourceId: "", search: "", future: true } }] }), /Unsupported condition/],
    ["corrupt JSON", "{broken", /Saved filters are corrupt/],
    ["invalid collection", JSON.stringify({ version: 1, filters: null }), /Saved filters are corrupt/],
    ["missing condition", JSON.stringify({ version: 1, filters: [{ name: "Incomplete", filters: { range: "24h" } }] }), /Invalid sourceId/],
  ])("reports %s without dropping data or overwriting it", (_name, stored, error) => {
    window.localStorage.setItem(SAVED_FILTERS_STORAGE_KEY, stored);
    expect(() => loadSavedFilters(window.localStorage)).toThrow(error);
    expect(() => saveFilter(window.localStorage, "New", { range: "1h", sourceId: "", search: "" })).toThrow(error);
    expect(window.localStorage.getItem(SAVED_FILTERS_STORAGE_KEY)).toBe(stored);
  });

  it("requires a usable unique name and an existing replacement or deletion target", () => {
    const filters: InvestigationFilters = { range: "24h", sourceId: "", search: "" };
    expect(() => saveFilter(window.localStorage, "   ", filters)).toThrow(/name/);
    saveFilter(window.localStorage, "Failures", filters);
    expect(() => saveFilter(window.localStorage, " Failures ", filters)).toThrow(/already exists/);
    expect(() => replaceFilter(window.localStorage, "Missing", filters)).toThrow(/no longer exists/);
    expect(() => deleteFilter(window.localStorage, "Missing")).toThrow(/no longer exists/);
    expect(loadSavedFilters(window.localStorage)).toEqual([{ name: "Failures", filters }]);
  });

  it.each(["save", "replace", "delete"] as const)("does not claim a successful %s when persistence fails", (operation) => {
    const filters: InvestigationFilters = { range: "24h", sourceId: "codex", search: "" };
    const before = saveFilter(window.localStorage, "Existing", filters);
    const unavailableStorage = {
      getItem: window.localStorage.getItem.bind(window.localStorage),
      setItem: () => { throw new Error("Storage is unavailable"); },
    };
    const mutations = {
      save: () => saveFilter(unavailableStorage, "Another", filters),
      replace: () => replaceFilter(unavailableStorage, "Existing", { ...filters, range: "7d" }),
      delete: () => deleteFilter(unavailableStorage, "Existing"),
    };
    expect(mutations[operation]).toThrow("Storage is unavailable");
    expect(loadSavedFilters(window.localStorage)).toEqual(before);
  });

  it("keeps relative conditions relative after time advances and preserves separately saved names", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-04T01:00:00Z"));
    const filters: InvestigationFilters = { range: "24h", sourceId: "codex", search: "" };
    saveFilter(window.localStorage, "Daily", filters);
    vi.setSystemTime(new Date("2026-09-05T12:00:00Z"));
    saveFilter(window.localStorage, "Weekly", { ...filters, range: "7d" });
    const replaced = replaceFilter(window.localStorage, "Daily", { ...filters, search: "retry" });
    expect(replaced).toEqual([
      { name: "Daily", filters: { ...filters, search: "retry" } },
      { name: "Weekly", filters: { ...filters, range: "7d" } },
    ]);
    expect(loadSavedFilters(window.localStorage)).toEqual(replaced);
  });
});
