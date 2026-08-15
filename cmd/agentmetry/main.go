package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/theoden9014/agentmetry/internal/app"
	"github.com/theoden9014/agentmetry/internal/compaction"
	"github.com/theoden9014/agentmetry/internal/planusage"
	"github.com/theoden9014/agentmetry/internal/source/builtin"
	store "github.com/theoden9014/agentmetry/internal/storage/sqlite"
	webassets "github.com/theoden9014/agentmetry/web"
)

type configuration struct {
	dashboardAddress string
	otlpHTTPAddress  string
	grpcAddress      string
	database         string
	compactDatabase  bool
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "import-plan-usage" {
		if err := runPlanUsageImport(os.Args[2:], os.Stdin, os.Stdout, http.DefaultClient); err != nil {
			slog.Error("plan usage import failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("agentmetry stopped", "error", err)
		os.Exit(1)
	}
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func runPlanUsageImport(arguments []string, input io.Reader, output io.Writer, client httpDoer) error {
	flags := flag.NewFlagSet("import-plan-usage", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var sourceID, endpoint string
	flags.StringVar(&sourceID, "source", "", "source parser ID")
	flags.StringVar(&endpoint, "endpoint", "http://127.0.0.1:17890/api/v1/plan-usage", "Agentmetry plan usage endpoint")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	parser, ok := builtin.PlanUsageParser(sourceID)
	if !ok {
		return fmt.Errorf("unsupported plan usage source %q", sourceID)
	}
	payload, err := io.ReadAll(io.LimitReader(input, 4<<20))
	if err != nil {
		return fmt.Errorf("read plan usage input: %w", err)
	}
	snapshots, err := parser.Parse(payload, time.Now())
	if err != nil {
		return err
	}
	if err := postPlanUsage(endpoint, sourceID, payload, client); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, planUsageSummary(snapshots))
	return err
}

func postPlanUsage(endpoint, source string, raw []byte, client httpDoer) error {
	body, err := json.Marshal(struct {
		Source string          `json:"source"`
		Raw    json.RawMessage `json:"raw"`
	}{Source: source, Raw: raw})
	if err != nil {
		return fmt.Errorf("encode plan usage snapshot: %w", err)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create plan usage request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("send plan usage snapshot: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("plan usage endpoint returned %s", response.Status)
	}
	return nil
}

func planUsageSummary(snapshots []planusage.Snapshot) string {
	parts := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		window := snapshot.WindowID
		if snapshot.WindowDurationMinutes > 0 && snapshot.WindowDurationMinutes%60 == 0 {
			window = fmt.Sprintf("%dh", snapshot.WindowDurationMinutes/60)
		}
		parts = append(parts, fmt.Sprintf("%s %.1f%% used", window, snapshot.UsedPercent))
	}
	return "Plan: " + strings.Join(parts, " · ")
}

func run() error {
	config := parseFlags()
	if err := os.MkdirAll(filepath.Dir(config.database), 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	if config.compactDatabase {
		result, err := compaction.Migrate(context.Background(), config.database, builtin.Registry(), func(progress compaction.Progress) {
			if progress.Completed%100 == 0 || progress.Completed == progress.Total {
				slog.Info("compacting telemetry database", "completed", progress.Completed, "total", progress.Total)
			}
		})
		if err != nil {
			return err
		}
		slog.Info("telemetry database compacted", "before_bytes", result.SourceBytes, "after_bytes", result.CompactBytes, "exports", result.Exports, "semantic_spans", result.SemanticSpans)
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errorsChannel := make(chan error, 3)
	maintenance := app.NewMaintenanceHandler()
	dashboardServer := &http.Server{
		Addr:              config.dashboardAddress,
		Handler:           maintenance,
		ReadHeaderTimeout: 5 * time.Second,
	}
	dashboardListener, err := net.Listen("tcp", config.dashboardAddress)
	if err != nil {
		return fmt.Errorf("reserve dashboard address before database migration: %w", err)
	}
	go func() {
		slog.Info("dashboard maintenance surface listening", "address", config.dashboardAddress)
		errorsChannel <- dashboardServer.Serve(dashboardListener)
	}()

	if _, err := compaction.MigrateIfNeeded(ctx, config.database, builtin.Registry(), func(progress compaction.Progress) {
		maintenance.Progress(progress.Completed, progress.Total)
		if progress.Completed%100 == 0 || progress.Completed == progress.Total {
			slog.Info("migrating telemetry database", "completed", progress.Completed, "total", progress.Total)
		}
	}); err != nil {
		migrationError := fmt.Errorf("migrate telemetry database: %w", err)
		maintenance.Fail(migrationError)
		select {
		case <-ctx.Done():
			_ = dashboardServer.Close()
			return migrationError
		case serverError := <-errorsChannel:
			return errors.Join(migrationError, serverError)
		}
	}
	database, err := store.Open(config.database, builtin.Registry())
	if err != nil {
		_ = dashboardServer.Close()
		return err
	}
	defer database.Close()

	services := app.NewServices(database, webassets.FS(), time.Now)
	maintenance.Ready(services.Dashboard)
	grpcServer := grpc.NewServer()
	services.OTLPReceiver.RegisterGRPC(grpcServer)
	grpcListener, err := net.Listen("tcp", config.grpcAddress)
	if err != nil {
		_ = dashboardServer.Close()
		return fmt.Errorf("listen for OTLP gRPC: %w", err)
	}

	otlpHTTPServer := &http.Server{
		Addr:              config.otlpHTTPAddress,
		Handler:           services.OTLPHTTPHandler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("OTLP gRPC listening", "address", config.grpcAddress)
		errorsChannel <- grpcServer.Serve(grpcListener)
	}()
	go func() {
		slog.Info("OTLP HTTP listening", "address", config.otlpHTTPAddress)
		errorsChannel <- otlpHTTPServer.ListenAndServe()
	}()
	slog.Info("dashboard, API, and MCP ready", "address", config.dashboardAddress)

	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		grpcServer.GracefulStop()
		if err := otlpHTTPServer.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown OTLP HTTP server: %w", err)
		}
		if err := dashboardServer.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown dashboard server: %w", err)
		}
		return nil
	case err := <-errorsChannel:
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	}
}

func parseFlags() configuration {
	config := configuration{}
	flag.StringVar(&config.dashboardAddress, "http-address", "127.0.0.1:17890", "dashboard, API, and MCP listen address")
	flag.StringVar(&config.otlpHTTPAddress, "otlp-http-address", "127.0.0.1:4318", "OTLP HTTP listen address")
	flag.StringVar(&config.grpcAddress, "otlp-grpc-address", "127.0.0.1:4317", "OTLP gRPC listen address")
	flag.StringVar(&config.database, "database", filepath.Join("data", "agentmetry.db"), "SQLite database path")
	flag.BoolVar(&config.compactDatabase, "compact-database", false, "compact a closed legacy database and exit")
	flag.Parse()
	return config
}
