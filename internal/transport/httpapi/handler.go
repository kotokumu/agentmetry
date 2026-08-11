package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/theoden9014/agentmetry/internal/planusage"
	"github.com/theoden9014/agentmetry/internal/query"
)

type Clock func() time.Time

type Reader interface {
	query.OverviewReader
	query.ConversationReader
	query.TraceReader
}

type API struct {
	reader Reader
	assets fs.FS
	now    Clock
	plans  planusage.RawImporter
}

func New(reader Reader, assets fs.FS, now Clock, planImporters ...planusage.RawImporter) http.Handler {
	api := &API{reader: reader, assets: assets, now: now}
	if len(planImporters) > 0 {
		api.plans = planImporters[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.health)
	mux.HandleFunc("GET /api/v1/overview", api.overview)
	mux.HandleFunc("GET /api/v1/session-activities", api.sessionActivities)
	mux.HandleFunc("GET /api/v1/conversations/{sourceId}/{conversationId}", api.conversation)
	mux.HandleFunc("GET /api/v1/traces/{traceId}", api.trace)
	if api.plans != nil {
		mux.HandleFunc("POST /api/v1/plan-usage", api.putPlanUsage)
	}
	mux.HandleFunc("/api/", http.NotFound)
	mux.HandleFunc("/", api.spa)
	return mux
}

func (api *API) conversation(response http.ResponseWriter, request *http.Request) {
	sourceID := strings.TrimSpace(request.PathValue("sourceId"))
	conversationID := strings.TrimSpace(request.PathValue("conversationId"))
	if sourceID == "" || conversationID == "" {
		writeJSONError(response, http.StatusBadRequest, fmt.Errorf("source and conversation identities are required"))
		return
	}
	if len(sourceID) > 100 || len(conversationID) > 500 {
		writeJSONError(response, http.StatusBadRequest, fmt.Errorf("conversation identity is too long"))
		return
	}
	traceID := strings.TrimSpace(request.URL.Query().Get("traceId"))
	spanID := strings.TrimSpace(request.URL.Query().Get("spanId"))
	if (traceID == "") != (spanID == "") {
		writeJSONError(response, http.StatusBadRequest, fmt.Errorf("traceId and spanId must be provided together"))
		return
	}
	if traceID != "" {
		var err error
		traceID, err = query.ParseTraceID(traceID)
		if err != nil {
			writeJSONError(response, http.StatusBadRequest, err)
			return
		}
		spanID, err = query.ParseSpanID(spanID)
		if err != nil {
			writeJSONError(response, http.StatusBadRequest, err)
			return
		}
	}
	offset, err := pageInteger(request, "offset", 0)
	if err != nil {
		writeJSONError(response, http.StatusBadRequest, err)
		return
	}
	limit, err := pageInteger(request, "limit", 100)
	if err != nil || limit < 1 || limit > 100 {
		writeJSONError(response, http.StatusBadRequest, fmt.Errorf("limit must be between 1 and 100"))
		return
	}
	pageMode := request.URL.Query().Get("mode")
	if pageMode != "" && pageMode != "page" {
		writeJSONError(response, http.StatusBadRequest, fmt.Errorf("mode must be page when provided"))
		return
	}
	conversation, err := api.reader.GetConversation(request.Context(), query.ConversationFilter{
		SourceID: sourceID, ConversationID: conversationID, TraceID: traceID, SpanID: spanID,
		ActivityOffset: offset, ActivityLimit: limit, UseActivityOffset: pageMode == "page",
	})
	if errors.Is(err, query.ErrConversationNotFound) || errors.Is(err, query.ErrConversationTargetNotFound) {
		writeJSONError(response, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeJSONError(response, http.StatusInternalServerError, fmt.Errorf("conversation unavailable"))
		return
	}
	conversation.Sources = nonNil(conversation.Sources)
	conversation.TraceIDs = nonNil(conversation.TraceIDs)
	conversation.Agents = nonNil(conversation.Agents)
	conversation.Activities = nonNil(conversation.Activities)
	writeJSON(response, http.StatusOK, struct {
		Version      string        `json:"version"`
		Conversation query.Session `json:"conversation"`
	}{Version: "v1", Conversation: conversation})
}

func (api *API) trace(response http.ResponseWriter, request *http.Request) {
	traceID, err := query.ParseTraceID(request.PathValue("traceId"))
	if err != nil {
		writeJSONError(response, http.StatusBadRequest, err)
		return
	}
	trace, err := api.reader.GetTrace(request.Context(), query.TraceFilter{TraceID: traceID})
	if errors.Is(err, query.ErrTraceNotFound) {
		writeJSONError(response, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeJSONError(response, http.StatusInternalServerError, fmt.Errorf("trace unavailable"))
		return
	}
	trace.Conversations = nonNil(trace.Conversations)
	trace.Agents = nonNil(trace.Agents)
	trace.Activities = nonNil(trace.Activities)
	writeJSON(response, http.StatusOK, struct {
		Version string      `json:"version"`
		Trace   query.Trace `json:"trace"`
	}{Version: "v1", Trace: trace})
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

func (api *API) sessionActivities(response http.ResponseWriter, request *http.Request) {
	sessionID := request.URL.Query().Get("sessionId")
	if sessionID == "" {
		writeJSONError(response, http.StatusBadRequest, fmt.Errorf("sessionId is required"))
		return
	}
	if request.URL.Query().Get("source") == "" {
		writeJSONError(response, http.StatusBadRequest, fmt.Errorf("source is required for source-qualified conversation identity"))
		return
	}
	filter, err := query.FilterForRange(api.now(), request.URL.Query().Get("range"))
	if err != nil {
		writeJSONError(response, http.StatusBadRequest, err)
		return
	}
	if err := applySessionFilters(request, &filter); err != nil {
		writeJSONError(response, http.StatusBadRequest, err)
		return
	}
	filter.ActivityOffset, err = pageInteger(request, "offset", 0)
	if err != nil {
		writeJSONError(response, http.StatusBadRequest, err)
		return
	}
	filter.ActivityLimit, err = pageInteger(request, "limit", 100)
	if err != nil || filter.ActivityLimit < 1 || filter.ActivityLimit > 100 {
		writeJSONError(response, http.StatusBadRequest, fmt.Errorf("limit must be between 1 and 100"))
		return
	}
	overview, err := api.reader.GetOverview(request.Context(), filter)
	if err != nil {
		writeJSONError(response, http.StatusInternalServerError, fmt.Errorf("session activities unavailable"))
		return
	}
	for _, session := range overview.Sessions {
		if session.ID != sessionID || session.SourceID != filter.SourceID {
			continue
		}
		writeJSON(response, http.StatusOK, struct {
			Activities []query.Activity `json:"activities"`
			Total      int64            `json:"total"`
			HasMore    bool             `json:"hasMore"`
		}{
			Activities: session.Activities,
			Total:      session.ActivityCount,
			HasMore:    int64(filter.ActivityOffset+len(session.Activities)) < session.ActivityCount,
		})
		return
	}
	writeJSONError(response, http.StatusNotFound, fmt.Errorf("session not found"))
}

func pageInteger(request *http.Request, name string, defaultValue int) (int, error) {
	value := request.URL.Query().Get(name)
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return parsed, nil
}

func (api *API) putPlanUsage(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Content-Type") != "application/json" {
		writeJSONError(response, http.StatusUnsupportedMediaType, fmt.Errorf("Content-Type must be application/json"))
		return
	}
	payload, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	if err != nil {
		writeJSONError(response, http.StatusBadRequest, fmt.Errorf("read plan usage snapshot"))
		return
	}
	var envelope struct {
		Source string          `json:"source"`
		Raw    json.RawMessage `json:"raw"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		writeJSONError(response, http.StatusBadRequest, fmt.Errorf("decode plan usage snapshot"))
		return
	}
	if envelope.Source == "" || len(envelope.Raw) == 0 {
		writeJSONError(response, http.StatusBadRequest, fmt.Errorf("source and raw plan usage payload are required"))
		return
	}
	snapshots, err := api.plans.ImportRaw(request.Context(), envelope.Source, envelope.Raw, api.now().UTC())
	if err != nil {
		writeJSONError(response, http.StatusBadRequest, err)
		return
	}
	writeJSON(response, http.StatusAccepted, struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}{Status: "accepted", Count: len(snapshots)})
}

func (api *API) health(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	_, _ = response.Write([]byte(`{"status":"ok"}`))
}

func (api *API) overview(response http.ResponseWriter, request *http.Request) {
	filter, err := query.FilterForRange(api.now(), request.URL.Query().Get("range"))
	if err != nil {
		writeJSONError(response, http.StatusBadRequest, err)
		return
	}
	if err := applySessionFilters(request, &filter); err != nil {
		writeJSONError(response, http.StatusBadRequest, err)
		return
	}
	overview, err := api.reader.GetOverview(request.Context(), filter)
	if err != nil {
		writeJSONError(response, http.StatusInternalServerError, fmt.Errorf("overview unavailable"))
		return
	}
	writeJSON(response, http.StatusOK, struct {
		Version  string         `json:"version"`
		Overview query.Overview `json:"overview"`
	}{Version: "v1", Overview: overview})
}

func applySessionFilters(request *http.Request, filter *query.OverviewFilter) error {
	filter.SourceID = strings.TrimSpace(request.URL.Query().Get("source"))
	filter.Search = strings.TrimSpace(request.URL.Query().Get("q"))
	if len(filter.SourceID) > 100 {
		return fmt.Errorf("source must be at most 100 characters")
	}
	if len(filter.Search) > 200 {
		return fmt.Errorf("q must be at most 200 characters")
	}
	return nil
}

func (api *API) spa(response http.ResponseWriter, request *http.Request) {
	assetPath := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
	if assetPath == "." || assetPath == "" {
		assetPath = "index.html"
	}
	payload, err := fs.ReadFile(api.assets, assetPath)
	if err != nil {
		payload, err = fs.ReadFile(api.assets, "index.html")
		assetPath = "index.html"
	}
	if err != nil {
		http.Error(response, "web UI unavailable", http.StatusServiceUnavailable)
		return
	}
	if contentType := mime.TypeByExtension(path.Ext(assetPath)); contentType != "" {
		response.Header().Set("Content-Type", contentType)
	}
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(payload)
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeJSONError(response http.ResponseWriter, status int, err error) {
	writeJSON(response, status, struct {
		Error string `json:"error"`
	}{Error: err.Error()})
}
