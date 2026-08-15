package app

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"sync"
)

type MaintenanceStatus struct {
	Status    string `json:"status"`
	Completed int64  `json:"completed,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Message   string `json:"message,omitempty"`
}

// MaintenanceHandler exposes startup progress until the database-backed
// application handler is ready, then atomically delegates every request.
type MaintenanceHandler struct {
	mu       sync.RWMutex
	status   MaintenanceStatus
	delegate http.Handler
}

func NewMaintenanceHandler() *MaintenanceHandler {
	return &MaintenanceHandler{status: MaintenanceStatus{Status: "migrating"}}
}

func (handler *MaintenanceHandler) Progress(completed, total int64) {
	handler.mu.Lock()
	handler.status = MaintenanceStatus{Status: "migrating", Completed: completed, Total: total}
	handler.mu.Unlock()
}

func (handler *MaintenanceHandler) Fail(err error) {
	handler.mu.Lock()
	handler.status = MaintenanceStatus{Status: "failed", Message: err.Error()}
	handler.mu.Unlock()
}

func (handler *MaintenanceHandler) Ready(delegate http.Handler) {
	handler.mu.Lock()
	handler.delegate = delegate
	handler.status = MaintenanceStatus{Status: "ok"}
	handler.mu.Unlock()
}

func (handler *MaintenanceHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	handler.mu.RLock()
	delegate := handler.delegate
	status := handler.status
	handler.mu.RUnlock()
	if delegate != nil {
		delegate.ServeHTTP(response, request)
		return
	}
	if request.URL.Path == "/healthz" {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(status)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(response, maintenanceHTML, progressPercent(status), html.EscapeString(status.Message))
}

func progressPercent(status MaintenanceStatus) int64 {
	if status.Total <= 0 {
		return 0
	}
	return status.Completed * 100 / status.Total
}

const maintenanceHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Agentmetry · Updating data</title><style>
:root{color-scheme:dark;font-family:ui-sans-serif,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#0f1218;color:#f5f7fb}
body{margin:0;min-height:100vh;display:grid;place-items:center}.card{width:min(520px,calc(100vw - 48px));padding:32px;border:1px solid #2a3240;border-radius:18px;background:#171c25;box-shadow:0 24px 80px #0007}
h1{font-size:22px;margin:0 0 10px}p{color:#aeb8c8;line-height:1.5}.track{height:8px;background:#293140;border-radius:99px;overflow:hidden;margin:24px 0 12px}.bar{height:100%%;width:%d%%;background:#7c9cff;transition:width .25s}.error{color:#ff9c9c;overflow-wrap:anywhere}
</style></head><body><main class="card"><h1>Updating Agentmetry data</h1><p>Your lossless telemetry journal is being verified. Conversations and usage views will be regenerated automatically.</p><div class="track"><div class="bar"></div></div><p id="status">Preparing migration…</p><p class="error">%s</p></main><script>
async function poll(){try{const r=await fetch('/healthz',{cache:'no-store'});const s=await r.json();if(s.status==='ok'){location.reload();return}if(s.status==='failed'){document.querySelector('.error').textContent=s.message;document.querySelector('#status').textContent='Migration paused. Your original database is unchanged.';return}const pct=s.total?Math.floor(s.completed*100/s.total):0;document.querySelector('.bar').style.width=pct+'%%';document.querySelector('#status').textContent=s.total?pct+'%% · '+s.completed.toLocaleString()+' / '+s.total.toLocaleString()+' exports verified':'Preparing migration…'}catch(e){}setTimeout(poll,700)}poll();
</script></body></html>`
