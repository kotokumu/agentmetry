import { invoke, isTauri } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";

export type UpdatePhase =
  | "checking"
  | "up-to-date"
  | "available"
  | "downloading"
  | "installing"
  | "restarting"
  | "failed";

export type UpdateCheckResult = Readonly<{
  available: boolean;
  currentVersion: string;
  version?: string;
}>;

export type AppUpdateEvent = Readonly<{
  phase: UpdatePhase;
  currentVersion?: string;
  version?: string;
  downloaded?: number;
  total?: number;
  message?: string;
}>;

export interface DesktopUpdater {
  readonly supported: boolean;
  check(): Promise<UpdateCheckResult>;
  install(): Promise<UpdateCheckResult>;
  subscribe(listener: (event: AppUpdateEvent) => void): Promise<() => void>;
}

class TauriDesktopUpdater implements DesktopUpdater {
  readonly supported = isTauri();

  check() {
    return invoke<UpdateCheckResult>("check_for_app_update");
  }

  install() {
    return invoke<UpdateCheckResult>("install_app_update");
  }

  subscribe(listener: (event: AppUpdateEvent) => void) {
    return listen<AppUpdateEvent>("agentmetry://update-status", ({ payload }) => listener(payload));
  }
}

export const desktopUpdater: DesktopUpdater = new TauriDesktopUpdater();
