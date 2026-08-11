# ADR 0011: Resident Application UX

- Status: Accepted
- Date: 2026-08-11
- Scope: Desktop window and tray behavior

## Decision

Agentmetry is a resident local telemetry receiver. Starting the desktop
application starts the Go sidecar, dashboard, MCP endpoint, and OTLP HTTP/gRPC
receivers together. The application does not expose separate user controls for
starting or stopping OTLP reception in this milestone.

The window is a view onto the resident process, not the process itself:

- close window: hide window, keep receiving OTLP;
- tray Open: show and focus the window;
- tray Hide: hide the window;
- tray Quit: stop the sidecar and exit the desktop application.

## Rationale

An initial `Start Agentmetry` menu was misleading because the sidecar was
already started during application launch, so pressing it caused no observable
change. Stopping the whole sidecar also stopped the HTTP server that served the
dashboard, leaving no usable place to explain the stopped state. Since OTLP
start/stop is not a required user workflow, the controls are removed rather
than introducing a second receiver lifecycle prematurely.

## UI behavior

The dashboard header shows a passive status:

```text
Receiving OTLP locally · HTTP :4318 · gRPC :4317
```

This is a status indicator, not a control. If the dashboard is visible, the
sidecar is serving the dashboard and the receiver was started as part of the
same startup sequence. The tray menu remains the authoritative place for
window visibility and application exit.

## Acceptance criteria

1. No tray item says `Start Agentmetry`, `Stop Agentmetry`, or implies that the
   dashboard window controls process startup.
2. Launching the app starts OTLP reception before the window is shown.
3. Closing and reopening the window does not restart or stop the sidecar.
4. Quitting the app terminates the sidecar and all OTLP listeners.
5. The dashboard explicitly communicates that local OTLP reception is active.
