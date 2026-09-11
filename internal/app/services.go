package app

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"time"

	"github.com/kotokumu/agentmetry/internal/archivefs"
	"github.com/kotokumu/agentmetry/internal/ingest"
	"github.com/kotokumu/agentmetry/internal/ingest/otel"
	"github.com/kotokumu/agentmetry/internal/planusage"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/retention"
	"github.com/kotokumu/agentmetry/internal/source/builtin"
	"github.com/kotokumu/agentmetry/internal/transport/connectapi"
	"github.com/kotokumu/agentmetry/internal/transport/httpapi"
	"github.com/kotokumu/agentmetry/internal/transport/mcpserver"
)

type Backend interface {
	ingest.ExportBatchCommitter
	query.OverviewReader
	query.ConversationReader
	query.DashboardReader
	query.SessionListReader
	query.SessionSummaryReader
	query.SessionActivitiesReader
	query.SessionReworkReader
	query.TraceReader
	query.ProjectionChangeReader
	query.ActivitySyncReader
	planusage.Writer
}

type Services struct {
	OTLPReceiver    *otel.Receiver
	OTLPHTTPHandler http.Handler
	Dashboard       http.Handler
	committer       *ingest.BatchingExportCommitter
	retention       *retention.Scheduler
}

func NewServices(backend Backend, assets fs.FS, now func() time.Time) Services {
	committer := ingest.NewBatchingExportCommitter(backend)
	receiver := otel.NewReceiver(committer, builtin.Registry())
	planImporter := planusage.NewImporter(backend, builtin.PlanUsageParser)
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpserver.New(backend, now))
	mux.Handle("/", httpapi.New(backend, assets, now, planImporter))
	connectPath, connectHandler := connectapi.New(backend, backend, now)
	mux.Handle(connectPath, connectHandler)
	var scheduler *retention.Scheduler
	if repository, ok := backend.(interface {
		retention.Repository
		ArchiveDirectory() string
	}); ok {
		retentionService := retention.NewServiceWithReplayer(repository, archivefs.New(repository.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), now)
		retentionPath, retentionHandler := connectapi.NewRetention(retentionService)
		mux.Handle(retentionPath, retentionHandler)
		scheduler = retention.StartScheduler(context.Background(), retentionService)
	}
	return Services{
		OTLPReceiver:    receiver,
		OTLPHTTPHandler: receiver.HTTPHandler(),
		Dashboard:       mux,
		committer:       committer,
		retention:       scheduler,
	}
}

func (services Services) Close(ctx context.Context) error {
	var retentionErr error
	if services.retention != nil {
		retentionErr = services.retention.Close(ctx)
	}
	return errors.Join(retentionErr, services.committer.Close(ctx))
}
