package app

import (
	"io/fs"
	"net/http"
	"time"

	"github.com/theoden9014/agentmetry/internal/ingest"
	"github.com/theoden9014/agentmetry/internal/ingest/otel"
	"github.com/theoden9014/agentmetry/internal/planusage"
	"github.com/theoden9014/agentmetry/internal/query"
	"github.com/theoden9014/agentmetry/internal/source/builtin"
	"github.com/theoden9014/agentmetry/internal/transport/connectapi"
	"github.com/theoden9014/agentmetry/internal/transport/httpapi"
	"github.com/theoden9014/agentmetry/internal/transport/mcpserver"
)

type Backend interface {
	ingest.ExportCommitter
	query.OverviewReader
	query.ConversationReader
	query.DashboardReader
	query.SessionListReader
	query.SessionSummaryReader
	query.SessionActivitiesReader
	query.TraceReader
	planusage.Writer
}

type Services struct {
	OTLPReceiver    *otel.Receiver
	OTLPHTTPHandler http.Handler
	Dashboard       http.Handler
}

func NewServices(backend Backend, assets fs.FS, now func() time.Time) Services {
	receiver := otel.NewReceiver(backend, builtin.Registry())
	planImporter := planusage.NewImporter(backend, builtin.PlanUsageParser)
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpserver.New(backend, now))
	mux.Handle("/", httpapi.New(backend, assets, now, planImporter))
	connectPath, connectHandler := connectapi.New(backend, now)
	mux.Handle(connectPath, connectHandler)
	return Services{
		OTLPReceiver:    receiver,
		OTLPHTTPHandler: receiver.HTTPHandler(),
		Dashboard:       mux,
	}
}
