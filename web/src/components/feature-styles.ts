import { css } from "lit";

export const featurePanelStyles = css`
  * { box-sizing: border-box; }
  .panel { position: relative; min-width: 0; max-width: 100%; border: 1px solid var(--am-border); border-radius: 12px; background: linear-gradient(145deg, rgba(18, 25, 35, .94), rgba(10, 15, 22, .94)); padding: 15px; box-shadow: inset 0 1px 0 rgba(255, 255, 255, .025), 0 14px 36px rgba(0, 0, 0, .16); }
  .panel::before { content: ""; position: absolute; inset: 0 auto auto 18px; width: 34px; height: 1px; background: var(--am-accent); box-shadow: 0 0 12px rgba(var(--am-accent-rgb), .55); }
  .eyebrow { margin: 0 0 6px; color: var(--am-accent); font: 700 .64rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .15em; text-transform: uppercase; }
  h2 { margin: 0 0 12px; font: 650 .82rem/1.2 Inter, ui-sans-serif, sans-serif; letter-spacing: .01em; }
  .error { color: var(--am-danger); }
  .empty { min-height: 280px; display: grid; place-items: center; text-align: center; color: var(--am-muted); }
  .empty h2 { color: var(--am-text); }
  .empty p { max-width: 46ch; line-height: 1.6; }
`;
