import { css } from "lit";

export const featurePanelStyles = css`
  * { box-sizing: border-box; }
  .panel { position: relative; min-width: 0; max-width: 100%; border: 1px solid var(--am-border); border-radius: 12px; background: var(--am-surface); padding: 15px; }

  .eyebrow { margin: 0 0 6px; color: var(--am-accent); font: 600 .8rem/1.3 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .02em; }
  h2 { margin: 0 0 12px; font: 650 1rem/1.3 Inter, ui-sans-serif, sans-serif; letter-spacing: .01em; }
  :focus-visible { outline: 2px solid var(--am-accent); outline-offset: 3px; }
  .error { color: var(--am-danger); }
  .empty { min-height: 280px; display: grid; place-items: center; text-align: center; color: var(--am-muted); }
  .empty h2 { color: var(--am-text); }
  .empty p { max-width: 46ch; line-height: 1.6; }
`;
