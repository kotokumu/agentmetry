import { css, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import { AgentmetryApp } from "../src/app/agentmetry-app";
import type { AppUpdateEvent, DesktopUpdater, UpdateCheckResult } from "../src/api/desktop-updater";
import "../src/components/connections-settings";
import { initializeLocale } from "../src/localization/localization";
import { LocalizedElement } from "../src/localization/localized-element";
import { initializeThemePreference, THEME_PREFERENCE_EVENT } from "../src/components/theme-preference";

type PreviewCheckResult = "current" | "available" | "failed";

class PreviewUpdater implements DesktopUpdater {
  readonly supported = true;
  private listeners = new Set<(event: AppUpdateEvent) => void>();
  result: PreviewCheckResult = "current";

  getVersion() {
    return Promise.resolve("1.17.1");
  }

  check(): Promise<UpdateCheckResult> {
    if (this.result === "failed") return Promise.reject(new Error("Simulated update check failure"));
    if (this.result === "available") return Promise.resolve({ available: true, currentVersion: "1.17.1", version: "1.18.0" });
    return Promise.resolve({ available: false, currentVersion: "1.17.1" });
  }

  install() {
    this.emit({ phase: "downloading", currentVersion: "1.17.1", version: "1.18.0", downloaded: 40, total: 100 });
    // The preview deliberately remains downloading until the user selects idle/remount.
    return new Promise<UpdateCheckResult>(() => undefined);
  }

  subscribe(listener: (event: AppUpdateEvent) => void) {
    this.listeners.add(listener);
    return Promise.resolve(() => this.listeners.delete(listener));
  }

  setPhase(phase: AppUpdateEvent["phase"]) {
    this.emit({ phase, currentVersion: "1.17.1", ...(phase === "available" || phase === "downloading" || phase === "installing" ? { version: "1.18.0" } : {}), ...(phase === "downloading" ? { downloaded: 40, total: 100 } : {}) });
  }

  private emit(event: AppUpdateEvent) {
    this.listeners.forEach((listener) => listener(event));
  }
}

@customElement("am-update-preview")
class UpdatePreview extends LocalizedElement {
  static styles = [AgentmetryApp.styles, css`
    :host { display: block; min-height: 100vh; }
    .preview-bar { max-width: 1800px; margin: 0 auto; padding: 16px clamp(16px, 1.5vw, 24px) 0; }
    .preview-notice { margin: 0 0 12px; padding: 10px 12px; border: 1px dashed var(--am-border-strong); border-radius: 8px; color: var(--am-muted); font-size: 13px; line-height: 1.45; }
    .preview-controls { display: flex; flex-wrap: wrap; align-items: end; gap: 10px 14px; margin-bottom: 14px; }
    .preview-controls label { display: grid; gap: 4px; color: var(--am-muted); font: 700 12px/1.2 "SFMono-Regular", "Cascadia Code", monospace; }
    .preview-controls select, .preview-controls button { min-height: 32px; border: 1px solid var(--am-border); border-radius: 7px; padding: 6px 9px; color: var(--am-text); background: var(--am-surface); font: 14px/1.2 Inter, ui-sans-serif, sans-serif; }
    .preview-controls button { cursor: pointer; }
    .preview-controls button:hover, .preview-controls button:focus-visible, .preview-controls select:focus-visible { border-color: var(--am-accent); outline: 2px solid var(--am-accent-soft); outline-offset: 2px; }
    .preview-controls .phase-controls { display: flex; flex-wrap: wrap; gap: 6px; }
    main { padding-top: 0; }
  `];

  @state() private checkResult: PreviewCheckResult = "current";
  @state() private settingsMounted = true;
  private readonly updater = new PreviewUpdater();
  private resetSequence = 0;

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener(THEME_PREFERENCE_EVENT, this.themeChanged);
    this.syncTheme();
  }

  disconnectedCallback() {
    window.removeEventListener(THEME_PREFERENCE_EVENT, this.themeChanged);
    super.disconnectedCallback();
  }

  render() {
    return html`<div class="preview-bar">
      <p class="preview-notice" lang="ja">開発用プレビュー：更新状態はシミュレーションです。実際のダウンロード・インストールは行いません。</p>
      <div class="preview-controls" aria-label="Update preview controls">
        <label>Simulated check result<select aria-label="Simulated check result" .value=${this.checkResult} @change=${this.checkResultChanged}>
          <option value="current">Current</option><option value="available">Available</option><option value="failed">Failure</option>
        </select></label>
        <div class="phase-controls" role="group" aria-label="Simulated update status">${(["idle", "checking", "up-to-date", "available", "downloading", "installing", "failed"] as const).map((phase) => html`<button type="button" @click=${phase === "idle" ? this.resetToIdle : () => this.updater.setPhase(phase)}>${phase}</button>`)}</div>
      </div>
    </div>
    <main>${this.settingsMounted ? html`<am-connections-settings .updater=${this.updater}></am-connections-settings>` : null}</main>`;
  }

  private readonly checkResultChanged = (event: Event) => {
    this.checkResult = (event.target as HTMLSelectElement).value as PreviewCheckResult;
    this.updater.result = this.checkResult;
  };

  private readonly themeChanged = () => this.syncTheme();

  private readonly resetToIdle = async () => {
    const sequence = ++this.resetSequence;
    this.settingsMounted = false;
    await this.updateComplete;
    if (sequence === this.resetSequence) this.settingsMounted = true;
  };

  private syncTheme() {
    this.dataset.theme = document.documentElement.dataset.theme === "light" ? "light" : "dark";
  }
}

await initializeLocale();
initializeThemePreference();

const root = document.querySelector<HTMLElement>("#preview-root");
if (root) root.append(document.createElement("am-update-preview"));
