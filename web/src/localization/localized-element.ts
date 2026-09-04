import { LitElement } from "lit";
import { updateWhenLocaleChanges } from "@lit/localize";

export class LocalizedElement extends LitElement {
  constructor() {
    super();
    updateWhenLocaleChanges(this);
  }
}
