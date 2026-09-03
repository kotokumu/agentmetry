import { parseInvestigationFilters, type InvestigationFilters } from "./investigation-conditions";

export type SavedFilter = Readonly<{ name: string; filters: InvestigationFilters }>;
export const SAVED_FILTERS_STORAGE_KEY = "agentmetry.saved-investigation-filters";

export function loadSavedFilters(storage: Pick<Storage, "getItem">): readonly SavedFilter[] {
  const text = storage.getItem(SAVED_FILTERS_STORAGE_KEY);
  if (text === null) return [];
  let decoded: unknown;
  try { decoded = JSON.parse(text); } catch { throw new Error("Saved filters are corrupt: invalid JSON."); }
  if (!decoded || typeof decoded !== "object" || Array.isArray(decoded)) throw new Error("Saved filters are corrupt: expected a saved collection.");
  const collection = decoded as Record<string, unknown>;
  if (collection.version !== 1) throw new Error("Unsupported saved filter version.");
  for (const key of Object.keys(collection)) {
    if (key !== "version" && key !== "filters") throw new Error(`Unsupported saved filter field: ${key}`);
  }
  if (!Array.isArray(collection.filters)) throw new Error("Saved filters are corrupt: expected a filter list.");
  const names = new Set<string>();
  return collection.filters.map((entry: unknown) => {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) throw new Error("Saved filters are corrupt: invalid filter entry.");
    const item = entry as Record<string, unknown>;
    for (const key of Object.keys(item)) {
      if (key !== "name" && key !== "filters") throw new Error(`Unsupported saved filter field: ${key}`);
    }
    const name = filterName(item.name);
    if (names.has(name)) throw new Error("Saved filters are corrupt: duplicate names.");
    names.add(name);
    return { name, filters: parseInvestigationFilters(item.filters) };
  });
}

export function saveFilter(storage: Pick<Storage, "getItem" | "setItem">, name: string, filters: InvestigationFilters): readonly SavedFilter[] {
  const current = loadSavedFilters(storage);
  const normalizedName = filterName(name);
  if (current.some((item) => item.name === normalizedName)) throw new Error("A saved filter with this name already exists. Use Replace with applied conditions.");
  const saved = [...current, { name: normalizedName, filters: parseInvestigationFilters(filters) }];
  storage.setItem(SAVED_FILTERS_STORAGE_KEY, JSON.stringify({ version: 1, filters: saved }));
  return saved;
}

export function replaceFilter(storage: Pick<Storage, "getItem" | "setItem">, name: string, filters: InvestigationFilters): readonly SavedFilter[] {
  const replacement = parseInvestigationFilters(filters);
  const current = loadSavedFilters(storage);
  if (!current.some((item) => item.name === name)) throw new Error("The saved filter no longer exists.");
  const saved = current.map((item) => item.name === name ? { name, filters: replacement } : item);
  storage.setItem(SAVED_FILTERS_STORAGE_KEY, JSON.stringify({ version: 1, filters: saved }));
  return saved;
}

export function deleteFilter(storage: Pick<Storage, "getItem" | "setItem">, name: string): readonly SavedFilter[] {
  const current = loadSavedFilters(storage);
  if (!current.some((item) => item.name === name)) throw new Error("The saved filter no longer exists.");
  const saved = current.filter((item) => item.name !== name);
  storage.setItem(SAVED_FILTERS_STORAGE_KEY, JSON.stringify({ version: 1, filters: saved }));
  return saved;
}

const filterName = (value: unknown): string => {
  if (typeof value !== "string" || !value.trim() || value.trim().length > 80) throw new Error("A saved filter name must contain 1–80 characters.");
  return value.trim();
};
