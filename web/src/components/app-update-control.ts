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

@customElement("am-app-update-control")
export class AppUpdateControl extends LocalizedElement {
  @property({ attribute: false }) updater: DesktopUpdater = desktopUpdater;
  @state() private phase: ControlPhase = "idle";
  @state() private currentVersion = "";
  @state() private availableVersion = "";
  @state() private downloaded?: number;
  @state() private total?: number;
  @state() private error = "";
  private unlisten?: () => void;
  private disconnected = false;

  static styles = css`
    :host { display: block; min-height: 31px; margin-bottom: 10px; }
    .control { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 8px 10px; }
    .message { margin: 0; color: var(--am-muted); font: .68rem/1.4 "SFMono-Regular", "Cascadia Code", monospace; }
    .message.available { color: var(--am-accent); }
    .message.error { color: var(--am-danger); }
    button { min-height: 31px; border: 1px solid var(--am-border); border-radius: 8px; padding: 7px 11px; color: var(--am-text); background: rgba(18, 25, 35, .82); font: 700 .64rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .04em; cursor: pointer; }
    button:hover:not(:disabled), button:focus-visible { border-color: var(--am-border-strong); color: var(--am-accent); background: var(--am-accent-soft); }
    button:focus-visible { outline: 2px solid var(--am-accent); outline-offset: 2px; }
    button:disabled { cursor: progress; opacity: .62; }
    button.primary { border-color: var(--am-border-strong); color: var(--am-accent); background: var(--am-accent-soft); }
    @media (max-width: 950px) { .control { justify-content: flex-start; } }
  `;

  connectedCallback() {
    super.connectedCallback();
    this.disconnected = false;
    if (!this.updater.supported) return;
    void this.updater
      .subscribe(this.updateStatusChanged)
      .then((unlisten) => {
        if (this.disconnected) unlisten();
        else this.unlisten = unlisten;
      })
      .catch((error) => this.fail(error));
  }

  disconnectedCallback() {
    this.disconnected = true;
    this.unlisten?.();
    this.unlisten = undefined;
    super.disconnectedCallback();
  }

  render() {
    if (!this.updater.supported) return nothing;
    const busy = ["checking", "downloading", "installing", "restarting"].includes(this.phase);
    return html`<div class="control" aria-live="polite">
      ${this.message()}
      <button
        class=${this.phase === "available" ? "primary" : ""}
        ?disabled=${busy}
        @click=${this.runAction}
      >${this.actionLabel()}</button>
    </div>`;
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
      this.availableVersion = result.version ?? "";
      this.phase = result.available ? "available" : "up-to-date";
    } catch (error) {
      this.fail(error);
    }
  }

  private readonly updateStatusChanged = (event: AppUpdateEvent) => {
    this.phase = event.phase;
    this.currentVersion = event.currentVersion ?? this.currentVersion;
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
