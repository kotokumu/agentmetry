import { css, html, nothing } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import {
  desktopUpdater,
  type AppUpdateEvent,
  type DesktopUpdater,
  type UpdatePhase,
} from "../api/desktop-updater";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";

type ControlPhase = "idle" | UpdatePhase;
type VersionState = "loading" | "available" | "unavailable";

@customElement("am-app-update-control")
export class AppUpdateControl extends LocalizedElement {
  @property({ attribute: false }) updater: DesktopUpdater = desktopUpdater;
  @state() private phase: ControlPhase = "idle";
  @state() private installedVersion = "";
  @state() private versionState: VersionState = "loading";
  @state() private currentVersion = "";
  @state() private availableVersion = "";
  @state() private downloaded?: number;
  @state() private total?: number;
  @state() private error = "";
  private unlisten?: () => void;
  private disconnected = false;
  private lifecycle = 0;

  static styles = css`
    :host { display: block; }
    .update-section { display: grid; gap: 12px; padding: 18px; border: 1px solid var(--am-border); border-radius: 10px; background: var(--am-surface-raised); }
    .update-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 16px; }
    h2 { margin: 0; color: var(--am-text); font-size: 1rem; }
    .installed-version { margin: 0; color: var(--am-text); font: .78rem/1.4 "SFMono-Regular", "Cascadia Code", monospace; text-align: right; }
    .installed-version span { color: var(--am-muted); }
    .installed-version code { color: var(--am-accent); }
    .control { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 8px 10px; }
    .message { margin: 0; color: var(--am-muted); font: 12px/1.4 "SFMono-Regular", "Cascadia Code", monospace; }
    .message.available { color: var(--am-accent); }
    .message.error { color: var(--am-danger); }
    button { min-height: 31px; border: 1px solid var(--am-border); border-radius: 8px; padding: 7px 11px; color: var(--am-text); background: var(--am-surface); font: 700 14px/1.1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .02em; cursor: pointer; }
    button:hover:not(:disabled), button:focus-visible { border-color: var(--am-border-strong); color: var(--am-accent); background: var(--am-accent-soft); }
    button:focus-visible { outline: 2px solid var(--am-accent); outline-offset: 2px; }
    button:disabled { cursor: progress; opacity: .62; }
    button.primary { border-color: var(--am-border-strong); color: var(--am-accent); background: var(--am-accent-soft); }
    @media (max-width: 950px) { .control { justify-content: flex-start; } }
    @media (max-width: 560px) { .update-heading { align-items: flex-start; flex-direction: column; gap: 7px; } .installed-version { text-align: left; } .control { align-items: flex-start; flex-direction: column; } }
  `;

  connectedCallback() {
    super.connectedCallback();
    this.disconnected = false;
    const lifecycle = ++this.lifecycle;
    if (!this.updater.supported) return;
    void this.loadInstalledVersion(lifecycle);
    void this.updater
      .subscribe(this.updateStatusChanged)
      .then((unlisten) => {
        if (this.disconnected || lifecycle !== this.lifecycle) unlisten();
        else {
          this.unlisten?.();
          this.unlisten = unlisten;
        }
      })
      .catch((error) => { if (!this.disconnected && lifecycle === this.lifecycle) this.fail(error); });
  }

  disconnectedCallback() {
    this.disconnected = true;
    this.lifecycle += 1;
    this.unlisten?.();
    this.unlisten = undefined;
    super.disconnectedCallback();
  }

  render() {
    if (!this.updater.supported) return nothing;
    const busy = ["checking", "downloading", "installing", "restarting"].includes(this.phase);
    return html`<section class="update-section" aria-labelledby="update-heading">
      <div class="update-heading"><h2 id="update-heading">${localization.t("update.heading")}</h2><p class="installed-version"><span>${localization.t("update.currentVersionLabel")}</span> ${this.versionText()}</p></div>
      <div class="control" aria-live="polite">
        ${this.message()}
        <button
          class=${this.phase === "available" ? "primary" : ""}
          ?disabled=${busy}
          @click=${this.runAction}
        >${this.actionLabel()}</button>
      </div>
    </section>`;
  }

  private async loadInstalledVersion(lifecycle: number) {
    if (!this.installedVersion) this.versionState = "loading";
    try {
      const version = await this.updater.getVersion();
      if (this.disconnected || lifecycle !== this.lifecycle) return;
      if (version) {
        this.installedVersion = version;
        this.versionState = "available";
      } else {
        this.versionState = this.installedVersion ? "available" : "unavailable";
      }
    } catch {
      if (this.disconnected || lifecycle !== this.lifecycle) return;
      this.versionState = this.installedVersion ? "available" : "unavailable";
    }
  }

  private versionText() {
    if (this.versionState === "loading") return localization.t("update.versionLoading");
    if (this.versionState === "unavailable") return localization.t("update.versionUnavailable");
    return html`<code>v${this.installedVersion}</code>`;
  }

  private readonly runAction = () => {
    if (this.phase === "available") void this.install();
    else void this.check();
  };

  private async check() {
    this.phase = "checking";
    this.error = "";
    try {
      const result = await this.updater.check();
      this.currentVersion = result.currentVersion;
      if (!this.installedVersion && result.currentVersion) {
        this.installedVersion = result.currentVersion;
        this.versionState = "available";
      }
      this.availableVersion = result.version ?? "";
      this.phase = result.available ? "available" : "up-to-date";
    } catch (error) {
      this.fail(error);
    }
  }

  private async install() {
    this.phase = "downloading";
    this.error = "";
    try {
      const result = await this.updater.install();
      this.currentVersion = result.currentVersion;
      if (!this.installedVersion && result.currentVersion) {
        this.installedVersion = result.currentVersion;
        this.versionState = "available";
      }
      this.availableVersion = result.version ?? "";
      this.phase = result.available ? "available" : "up-to-date";
    } catch (error) {
      this.fail(error);
    }
  }

  private readonly updateStatusChanged = (event: AppUpdateEvent) => {
    this.phase = event.phase;
    this.currentVersion = event.currentVersion ?? this.currentVersion;
    if (!this.installedVersion && event.currentVersion) {
      this.installedVersion = event.currentVersion;
      this.versionState = "available";
    }
    this.availableVersion = event.version ?? this.availableVersion;
    this.downloaded = event.downloaded;
    this.total = event.total;
    this.error = event.message ?? "";
  };

  private fail(error: unknown) {
    this.phase = "failed";
    this.error = error instanceof Error ? error.message : String(error);
  }

  private message() {
    switch (this.phase) {
      case "checking": return html`<p class="message">${localization.t("update.checkingMessage")}</p>`;
      case "up-to-date": return html`<p class="message">${localization.t("update.upToDate", { version: this.currentVersion })}</p>`;
      case "available": return html`<p class="message available">${localization.t("update.available", { version: this.availableVersion })}</p>`;
      case "downloading": return html`<p class="message">${localization.t("update.downloading", { version: this.availableVersion, progress: this.progress() })}</p>`;
      case "installing": return html`<p class="message">${localization.t("update.installing", { version: this.availableVersion })}</p>`;
      case "restarting": return html`<p class="message">${localization.t("update.restarting")}</p>`;
      case "failed": return html`<p class="message error">${this.error || localization.t("update.failed")}</p>`;
      default: return nothing;
    }
  }

  private progress() {
    if (this.downloaded === undefined || !this.total) return "…";
    return `${Math.min(100, Math.round((this.downloaded / this.total) * 100))}%`;
  }

  private actionLabel() {
    switch (this.phase) {
      case "checking": return localization.t("update.checkingAction");
      case "available": return localization.t("update.installAction");
      case "downloading": return localization.t("update.downloadingAction");
      case "installing": return localization.t("update.installingAction");
      case "restarting": return localization.t("update.restartingAction");
      case "failed": return localization.t("update.retryAction");
      default: return localization.t("update.checkAction");
    }
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "am-app-update-control": AppUpdateControl;
  }
}
