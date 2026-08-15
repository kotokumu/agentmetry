import { afterEach, describe, expect, it, vi } from "vitest";
import "./app-update-control";
import type {
  AppUpdateEvent,
  DesktopUpdater,
  UpdateCheckResult,
} from "../api/desktop-updater";
import type { AppUpdateControl } from "./app-update-control";

const updateResult = (overrides: Partial<UpdateCheckResult> = {}): UpdateCheckResult => ({
  available: false,
  currentVersion: "1.0.2",
  ...overrides,
});

const updaterStub = (overrides: Partial<DesktopUpdater> = {}) => {
  let listener: ((event: AppUpdateEvent) => void) | undefined;
  const updater: DesktopUpdater = {
    supported: true,
    check: vi.fn().mockResolvedValue(updateResult()),
    install: vi.fn().mockResolvedValue(undefined),
    subscribe: vi.fn().mockImplementation(async (next) => {
      listener = next;
      return () => undefined;
    }),
    ...overrides,
  };
  return { updater, emit: (event: AppUpdateEvent) => listener?.(event) };
};

const renderControl = async (updater: DesktopUpdater) => {
  const control = document.createElement("am-app-update-control") as AppUpdateControl;
  control.updater = updater;
  document.body.append(control);
  await control.updateComplete;
  return control;
};

afterEach(() => document.body.replaceChildren());

describe("app update control", () => {
  it("does not render outside the Tauri desktop runtime", async () => {
    const { updater } = updaterStub({ supported: false });
    const control = await renderControl(updater);

    expect(control.shadowRoot?.querySelector("button")).toBeNull();
    expect(control.shadowRoot?.textContent?.trim()).toBe("");
  });

  it("reports when the installed version is current", async () => {
    const { updater } = updaterStub();
    const control = await renderControl(updater);

    control.shadowRoot?.querySelector<HTMLButtonElement>("button")?.click();
    await vi.waitFor(() => expect(control.shadowRoot?.textContent).toContain("v1.0.2 is up to date"));
    expect(updater.check).toHaveBeenCalledOnce();
  });

  it("offers an available version and installs it on confirmation", async () => {
    const { updater } = updaterStub({
      check: vi.fn().mockResolvedValue(updateResult({ available: true, version: "1.1.0" })),
    });
    const control = await renderControl(updater);

    control.shadowRoot?.querySelector<HTMLButtonElement>("button")?.click();
    await vi.waitFor(() => expect(control.shadowRoot?.textContent).toContain("v1.1.0 available"));
    control.shadowRoot?.querySelector<HTMLButtonElement>("button")?.click();
    await vi.waitFor(() => expect(updater.install).toHaveBeenCalledOnce());
  });

  it("shows progress emitted by automatic or manual updates", async () => {
    const { updater, emit } = updaterStub();
    const control = await renderControl(updater);

    emit({ phase: "downloading", version: "1.1.0", downloaded: 25, total: 100 });
    await control.updateComplete;

    expect(control.shadowRoot?.textContent).toContain("Downloading v1.1.0 · 25%");
    expect(control.shadowRoot?.querySelector<HTMLButtonElement>("button")?.disabled).toBe(true);
  });

  it("keeps the update action retryable after a failed check", async () => {
    const { updater } = updaterStub({
      check: vi.fn().mockRejectedValue(new Error("network unavailable")),
    });
    const control = await renderControl(updater);

    control.shadowRoot?.querySelector<HTMLButtonElement>("button")?.click();
    await vi.waitFor(() => expect(control.shadowRoot?.textContent).toContain("network unavailable"));
    expect(control.shadowRoot?.querySelector<HTMLButtonElement>("button")?.disabled).toBe(false);
    expect(control.shadowRoot?.querySelector<HTMLButtonElement>("button")?.textContent).toContain("Try again");
  });
});
