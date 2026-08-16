package mcpserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/planusage"
	"github.com/theoden9014/agentmetry/internal/product"
	"github.com/theoden9014/agentmetry/internal/query"
)

type Clock func() time.Time

type AgentContextInput struct{}

type AgentContextOutput struct {
	ContractVersion   string              `json:"contractVersion"`
	CallerIdentity    CallerIdentity      `json:"callerIdentity"`
	RunIdentity       RunIdentityContract `json:"runIdentity"`
	DataDomains       []DataDomain        `json:"dataDomains"`
	Tools             []ToolCapability    `json:"tools"`
	Workflow          []string            `json:"recommendedWorkflow"`
	Limits            []string            `json:"limits"`
	UnavailableForNow []string            `json:"unavailableForNow"`
}

type CallerIdentity struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

type RunIdentityContract struct {
	Required         []string `json:"required"`
	LatestIsImplicit bool     `json:"latestIsImplicit"`
	Description      string   `json:"description"`
}

type DataDomain struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Fields      []string `json:"fields"`
	Caveats     []string `json:"caveats"`
}

type ToolCapability struct {
	Name     string   `json:"name"`
	Purpose  string   `json:"purpose"`
	Required []string `json:"required"`
	Optional []string `json:"optional"`
	Returns  []string `json:"returns"`
}

type RunContextInput struct {
	Source string `json:"source" jsonschema:"Required telemetry source ID, such as claude or codex."`
	RunID  string `json:"runId" jsonschema:"Required source-qualified session or run ID."`
}

type RunContextOutput struct {
	Context  RunContext             `json:"context"`
	Metadata AnalysisMetadataOutput `json:"metadata"`
}

type RunContext struct {
	SourceID  string               `json:"sourceId"`
	RunID     string               `json:"runId"`
	TraceIDs  []string             `json:"traceIds"`
	Agents    []AgentSessionOutput `json:"agents"`
	StartedAt time.Time            `json:"startedAt"`
	EndedAt   time.Time            `json:"endedAt"`
}

type AnalysisMetadataOutput struct {
	RuleVersion        string `json:"ruleVersion"`
	Confidence         string `json:"confidence"`
	SourceCompleteness string `json:"sourceCompleteness"`
	SourceCoverage     string `json:"sourceCoverage"`
	RedactionState     string `json:"redactionState"`
	Note               string `json:"note,omitempty"`
}

type EfficiencyOutput struct {
	WallDurationMs    int64   `json:"wallDurationMs"`
	ActiveDurationMs  int64   `json:"activeDurationMs"`
	ParallelismFactor float64 `json:"parallelismFactor"`
	Complete          bool    `json:"complete"`
	ActivityCoverage  string  `json:"activityCoverage"`
}

type RunSummaryOutput struct {
	Run        SessionOutput          `json:"run"`
	Efficiency EfficiencyOutput       `json:"efficiency"`
	Evidence   []query.Evidence       `json:"evidence"`
	Metadata   AnalysisMetadataOutput `json:"metadata"`
}

type TokenAgentOutput struct {
	AgentID         string           `json:"agentId"`
	AgentDefinition string           `json:"agentDefinition,omitempty"`
	AgentType       string           `json:"agentType,omitempty"`
	ParentAgentID   string           `json:"parentAgentId,omitempty"`
	Model           string           `json:"model,omitempty"`
	ActivityCount   int64            `json:"activityCount"`
	Tokens          TokenUsageOutput `json:"tokens"`
}

type TokenUsageDataOutput struct {
	SourceID string                 `json:"sourceId"`
	RunID    string                 `json:"runId"`
	Total    TokenUsageOutput       `json:"total"`
	ByAgent  []TokenAgentOutput     `json:"byAgent"`
	Evidence []query.Evidence       `json:"evidence"`
	Metadata AnalysisMetadataOutput `json:"metadata"`
}

type FindingOutput struct {
	ID                 string           `json:"id"`
	Kind               string           `json:"kind"`
	Severity           string           `json:"severity"`
	Summary            string           `json:"summary"`
	Metric             string           `json:"metric"`
	Value              float64          `json:"value"`
	Unit               string           `json:"unit"`
	Confidence         string           `json:"confidence"`
	SourceCompleteness string           `json:"sourceCompleteness"`
	Evidence           []query.Evidence `json:"evidence"`
}

type FindingsOutput struct {
	SourceID string                 `json:"sourceId"`
	RunID    string                 `json:"runId"`
	Findings []FindingOutput        `json:"findings"`
	Metadata AnalysisMetadataOutput `json:"metadata"`
}

type APIRetryWasteOutput struct {
	Attempts   int64            `json:"attempts"`
	DurationMs int64            `json:"durationMs"`
	Tokens     TokenUsageOutput `json:"tokens"`
}

type ReworkMetricsOutput struct {
	ValidationFailures            int64               `json:"validationFailures"`
	FailFixRetryCycles            int64               `json:"failFixRetryCycles"`
	ReworkDurationMs              int64               `json:"reworkDurationMs"`
	TotalAgentEffortMs            int64               `json:"totalAgentEffortMs"`
	ReworkAgentEffortRate         *float64            `json:"reworkAgentEffortRate"`
	ReworkTokens                  TokenUsageOutput    `json:"reworkTokens"`
	ToolAttemptsWithOutcome       int64               `json:"toolAttemptsWithOutcome"`
	ToolFailures                  int64               `json:"toolFailures"`
	ToolFailureRate               *float64            `json:"toolFailureRate"`
	APIRetryWaste                 APIRetryWasteOutput `json:"apiRetryWaste"`
	RepeatedCommands              int64               `json:"repeatedCommands"`
	ReeditedFiles                 int64               `json:"reeditedFiles"`
	ValidationAttemptsWithOutcome int64               `json:"validationAttemptsWithOutcome"`
	FirstPassEligibleValidations  int64               `json:"firstPassEligibleValidations"`
	FirstPassSuccesses            int64               `json:"firstPassSuccesses"`
	FirstPassSuccessRate          *float64            `json:"firstPassSuccessRate"`
	RecurringFailureLoops         int64               `json:"recurringFailureLoops"`
	RepeatedFailureAttempts       int64               `json:"repeatedFailureAttempts"`
	ResolvedFailureLoops          int64               `json:"resolvedFailureLoops"`
	UnresolvedFailureLoops        int64               `json:"unresolvedFailureLoops"`
	FailureResolutionDurationMs   int64               `json:"failureResolutionDurationMs"`
	FailureResolutionTokens       TokenUsageOutput    `json:"failureResolutionTokens"`
}

type ReworkAnalysisOutput struct {
	SourceID        string                          `json:"sourceId"`
	RunID           string                          `json:"runId"`
	Metrics         ReworkMetricsOutput             `json:"metrics"`
	Cycles          []query.ReworkCycle             `json:"cycles"`
	FailureEpisodes []query.RecurringFailureEpisode `json:"failureEpisodes"`
	Coverage        query.ReworkCoverage            `json:"coverage"`
	Capabilities    query.ReworkCapabilities        `json:"capabilities"`
	Metadata        AnalysisMetadataOutput          `json:"metadata"`
}

type CompareRunsInput struct {
	Runs       []RunReference `json:"runs" jsonschema:"Required list of source-qualified runs. Maximum 10."`
	Dimensions []string       `json:"dimensions,omitempty" jsonschema:"Optional metrics: wallDuration, activityCount, agentCount, totalTokens, costUsd."`
}

type RunReference struct {
	Source string `json:"source"`
	RunID  string `json:"runId"`
}

type ComparedRunOutput struct {
	SourceID       string    `json:"sourceId"`
	RunID          string    `json:"runId"`
	StartedAt      time.Time `json:"startedAt"`
	EndedAt        time.Time `json:"endedAt"`
	WallDurationMs int64     `json:"wallDurationMs"`
	ActivityCount  int64     `json:"activityCount"`
	AgentCount     int64     `json:"agentCount"`
	TotalTokens    *int64    `json:"totalTokens"`
	CostUSD        *float64  `json:"costUsd,omitempty"`
}

type CompareRunsOutput struct {
	Runs       []ComparedRunOutput    `json:"runs"`
	Dimensions []string               `json:"dimensions"`
	Evidence   []query.Evidence       `json:"evidence"`
	Metadata   AnalysisMetadataOutput `json:"metadata"`
}

type SourceCapabilitiesInput struct {
	Source string `json:"source,omitempty" jsonschema:"Optional source ID such as claude or codex."`
}

type SourceCapabilityOutput struct {
	Source      string `json:"source"`
	Capability  string `json:"capability"`
	State       string `json:"state"`
	Description string `json:"description"`
	Evidence    string `json:"evidence"`
}

type SourceCapabilitiesOutput struct {
	Capabilities []SourceCapabilityOutput `json:"capabilities"`
	Metadata     AnalysisMetadataOutput   `json:"metadata"`
}

type OverviewInput struct {
	Range     string `json:"range,omitempty" jsonschema:"Time range: 1h, 24h, or 7d. Defaults to 24h."`
	Source    string `json:"source,omitempty" jsonschema:"Optional telemetry source ID, such as one returned in overview.sources."`
	Search    string `json:"search,omitempty" jsonschema:"Optional case-insensitive full-session text search."`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"Maximum number of sessions to return. Defaults to 100; capped at 100."`
	PageToken string `json:"pageToken,omitempty" jsonschema:"Opaque continuation token returned by the previous call."`
}

type TraceInput struct {
	TraceID        string `json:"traceId" jsonschema:"Required OTLP trace ID."`
	PageSize       int    `json:"pageSize,omitempty" jsonschema:"Maximum number of trace activities to return. Defaults to 100; capped at 100."`
	PageToken      string `json:"pageToken,omitempty" jsonschema:"Opaque continuation token returned by the previous call."`
	IncludeContent bool   `json:"includeContent,omitempty" jsonschema:"Opt in to captured activity bodies. Defaults to false."`
}

type SessionActivitiesInput struct {
	Source         string `json:"source" jsonschema:"Required telemetry source ID."`
	SessionID      string `json:"sessionId" jsonschema:"Required session ID."`
	AgentID        string `json:"agentId,omitempty" jsonschema:"Optional agent runtime ID returned by get_session."`
	PageSize       int    `json:"pageSize,omitempty" jsonschema:"Maximum number of activities to return. Defaults to 100; capped at 100."`
	PageToken      string `json:"pageToken,omitempty" jsonschema:"Opaque continuation token returned by the previous call."`
	Direction      string `json:"direction,omitempty" jsonschema:"older. Defaults to older; newer is not supported."`
	IncludeContent bool   `json:"includeContent,omitempty" jsonschema:"Opt in to captured activity bodies. Defaults to false."`
}

type RunTimelineInput struct {
	Source         string `json:"source" jsonschema:"Required telemetry source ID."`
	RunID          string `json:"runId" jsonschema:"Required source-qualified run/session ID."`
	AgentID        string `json:"agentId,omitempty" jsonschema:"Optional agent runtime ID returned by get_run_context."`
	PageSize       int    `json:"pageSize,omitempty" jsonschema:"Maximum number of activities to return. Defaults to 100; capped at 100."`
	PageToken      string `json:"pageToken,omitempty" jsonschema:"Opaque continuation token returned by the previous call."`
	Direction      string `json:"direction,omitempty" jsonschema:"older. Defaults to older; newer is not supported."`
	IncludeContent bool   `json:"includeContent,omitempty" jsonschema:"Opt in to captured activity bodies. Defaults to false."`
}

type SessionActivitiesOutput struct {
	Source            string           `json:"source"`
	SessionID         string           `json:"sessionId"`
	Activities        []ActivityOutput `json:"activities"`
	Total             int64            `json:"total"`
	NextPageToken     string           `json:"nextPageToken,omitempty"`
	PreviousPageToken string           `json:"previousPageToken,omitempty"`
	HasEarlier        bool             `json:"hasEarlier"`
	HasMore           bool             `json:"hasMore"`
}

type TokenUsageOutput struct {
	Input      *int64 `json:"input"`
	Output     *int64 `json:"output"`
	CacheRead  *int64 `json:"cacheRead"`
	CacheWrite *int64 `json:"cacheWrite"`
	Reasoning  *int64 `json:"reasoning"`
	Total      *int64 `json:"total"`
}

type ActivityOutput struct {
	Source             string           `json:"source"`
	Signal             string           `json:"signal"`
	TraceID            string           `json:"traceId,omitempty"`
	SpanID             string           `json:"spanId,omitempty"`
	ParentSpanID       string           `json:"parentSpanId,omitempty"`
	Name               string           `json:"name"`
	Kind               string           `json:"kind"`
	ToolName           string           `json:"toolName,omitempty"`
	TargetAgentID      string           `json:"targetAgentId,omitempty"`
	TargetAgentType    string           `json:"targetAgentType,omitempty"`
	Content            string           `json:"content,omitempty"`
	ContentState       string           `json:"contentState"`
	AgentID            string           `json:"agentId"`
	AgentDefinition    string           `json:"agentDefinition,omitempty"`
	AgentType          string           `json:"agentType,omitempty"`
	ParentAgentID      string           `json:"parentAgentId,omitempty"`
	RunID              string           `json:"runId"`
	Model              string           `json:"model"`
	StartedAt          time.Time        `json:"startedAt"`
	EndedAt            time.Time        `json:"endedAt"`
	ObservedAt         time.Time        `json:"observedAt"`
	Status             string           `json:"status,omitempty"`
	Tokens             TokenUsageOutput `json:"tokens"`
	CostUSD            *float64         `json:"costUsd,omitempty"`
	ContributesToTotal bool             `json:"contributesToTotal"`
}

type AgentSessionOutput struct {
	AgentID         string           `json:"agentId"`
	AgentDefinition string           `json:"agentDefinition,omitempty"`
	AgentType       string           `json:"agentType,omitempty"`
	ParentAgentID   string           `json:"parentAgentId,omitempty"`
	Model           string           `json:"model,omitempty"`
	ActivityCount   int64            `json:"activityCount"`
	Tokens          TokenUsageOutput `json:"tokens"`
}

type SessionOutput struct {
	ID            string                  `json:"id"`
	SourceID      string                  `json:"sourceId"`
	Sources       []query.TelemetrySource `json:"sources"`
	TraceIDs      []string                `json:"traceIds"`
	StartedAt     time.Time               `json:"startedAt"`
	EndedAt       time.Time               `json:"endedAt"`
	ActivityCount int64                   `json:"activityCount"`
	Tokens        TokenUsageOutput        `json:"tokens"`
	CostUSD       *float64                `json:"costUsd,omitempty"`
	Agents        []AgentSessionOutput    `json:"agents"`
	Activities    []ActivityOutput        `json:"activities"`
}

type SignalCountsOutput struct {
	Traces  int64 `json:"traces"`
	Logs    int64 `json:"logs"`
	Metrics int64 `json:"metrics"`
}

type OverviewDataOutput struct {
	Sources        []query.TelemetrySource `json:"sources"`
	SignalCounts   SignalCountsOutput      `json:"signalCounts"`
	RunCount       int64                   `json:"runCount"`
	AgentCount     int64                   `json:"agentCount"`
	Tokens         TokenUsageOutput        `json:"tokens"`
	RecentActivity []ActivityOutput        `json:"recentActivity"`
	Sessions       []SessionOutput         `json:"sessions"`
	PlanUsage      []planusage.Snapshot    `json:"planUsage"`
}

type OverviewOutput struct {
	Overview          OverviewDataOutput `json:"overview"`
	NextPageToken     string             `json:"nextPageToken,omitempty"`
	PreviousPageToken string             `json:"previousPageToken,omitempty"`
}

type TraceDataOutput struct {
	TraceID            string                  `json:"traceId"`
	StartedAt          time.Time               `json:"startedAt"`
	EndedAt            time.Time               `json:"endedAt"`
	Status             string                  `json:"status"`
	RootSpanCount      int64                   `json:"rootSpanCount"`
	MissingParentCount int64                   `json:"missingParentCount"`
	Conversations      []query.ConversationRef `json:"conversations"`
	Agents             []query.TraceAgent      `json:"agents"`
	Activities         []ActivityOutput        `json:"activities"`
	ActivityOffset     int                     `json:"activityOffset"`
	ActivityCount      int64                   `json:"activityCount"`
	HasMore            bool                    `json:"hasMore"`
	NextPageToken      string                  `json:"nextPageToken,omitempty"`
	PreviousPageToken  string                  `json:"previousPageToken,omitempty"`
}

type TraceOutput struct {
	Trace TraceDataOutput `json:"trace"`
}

type Reader interface {
	query.DashboardReader
	query.SessionListReader
	query.SessionSummaryReader
	query.SessionActivitiesReader
	query.TraceReader
}

type Service struct {
	dashboardReader query.DashboardReader
	sessionReader   query.SessionListReader
	summaryReader   query.SessionSummaryReader
	activityReader  query.SessionActivitiesReader
	traceReader     query.TraceReader
	now             Clock
}

func New(reader Reader, now Clock) http.Handler {
	service := &Service{dashboardReader: reader, sessionReader: reader, summaryReader: reader, activityReader: reader, traceReader: reader, now: now}
	server := mcp.NewServer(implementationInfo(), &mcp.ServerOptions{Instructions: "Call get_agent_context first. The server is read-only and stateless; never assume the latest run is the caller's run. Provide source and runId explicitly for analysis."})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_agent_context", Title: "Get agent context contract",
		Description: "Describes what an AI caller can retrieve from Agentmetry, required run identity, completeness, privacy, and recommended analysis workflow.",
	}, service.getAgentContext)
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_run_context", Title: "Verify run context",
		Description: "Verifies a source-qualified run identity and returns its observed traces and agent topology. The caller must provide source and runId.",
	}, service.getRunContext)
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_runs", Title: "List development runs",
		Description: "Lists bounded source-qualified development runs with dashboard aggregates. It does not identify the caller's own run implicitly.",
	}, service.getOverview)
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_run_summary", Title: "Get run summary",
		Description: "Returns one source-qualified run summary, agent topology, observed token usage, and bounded efficiency indicators.",
	}, service.getRunSummary)
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_run_timeline", Title: "Get run timeline",
		Description: "Returns a bounded source-qualified activity page. Activity bodies are excluded unless includeContent is true.",
	}, service.getRunTimeline)
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_token_usage", Title: "Get token usage",
		Description: "Returns observed typed token usage for one run and each observed agent, preserving unavailable values.",
	}, service.getTokenUsage)
	mcp.AddTool(server, &mcp.Tool{
		Name: "find_bottlenecks", Title: "Find observed bottlenecks",
		Description: "Finds long observed activities for one run. Results are evidence-backed and may be partial when the run has more than one page.",
	}, service.findBottlenecks)
	mcp.AddTool(server, &mcp.Tool{
		Name: "find_coordination_risks", Title: "Find coordination risks",
		Description: "Finds only explicit error and missing-delegation-target evidence for one run; it does not infer hidden coordination failures.",
	}, service.findCoordinationRisks)
	mcp.AddTool(server, &mcp.Tool{
		Name: "analyze_rework", Title: "Analyze session rework",
		Description: "Normalizes producer telemetry and calculates evidence-backed failure, retry, rework, tool failure, API waste, repeated-command, and repeated-file-edit indicators for one explicit run.",
	}, service.analyzeRework)
	mcp.AddTool(server, &mcp.Tool{
		Name: "compare_runs", Title: "Compare development runs",
		Description: "Compares up to 10 explicitly named source-qualified runs using observed summary metrics.",
	}, service.compareRuns)
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_source_capabilities", Title: "Get source capabilities",
		Description: "Explains source-specific observability limits for Claude Code and Codex; unavailable fields are not treated as zero.",
	}, service.getSourceCapabilities)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_agent_sessions",
		Title:       "Get agent sessions",
		Description: "Returns bounded dashboard aggregates and session summaries. Operations and messages are intentionally fetched separately.",
	}, service.getOverview)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_session_activities",
		Title:       "Get session activities",
		Description: "Returns one bounded page of operations and messages for a source-qualified session.",
	}, service.getSessionActivities)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_trace",
		Title:       "Get trace",
		Description: "Returns a trace-wide causal view with spans, correlated logs, conversations, agents, status, and missing-parent evidence.",
	}, service.getTrace)
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Stateless:                    true,
			JSONResponse:                 true,
			PropagateRequestCancellation: true,
		},
	)
}

func implementationInfo() *mcp.Implementation {
	return &mcp.Implementation{
		Name:        product.ID,
		Title:       product.Name,
		Description: product.Description + " Call get_agent_context first; never assume the latest run is the caller's run.",
		Version:     product.Version,
	}
}

func (service *Service) getAgentContext(context.Context, *mcp.CallToolRequest, AgentContextInput) (*mcp.CallToolResult, AgentContextOutput, error) {
	return nil, AgentContextOutput{
		ContractVersion: "v1",
		CallerIdentity: CallerIdentity{
			Available: false,
			Reason:    "MCP does not reliably carry the caller's current Agentmetry source/session identity; provide source and runId explicitly.",
		},
		RunIdentity: RunIdentityContract{
			Required: []string{"source", "runId"}, LatestIsImplicit: false,
			Description: "Run identity is source-qualified. A list result or latest timestamp is not proof that the run belongs to the caller.",
		},
		DataDomains: []DataDomain{
			{Name: "run", Description: "Source-qualified development session summary.", Fields: []string{"sourceId", "runId", "startedAt", "endedAt", "activityCount", "traceIds"}, Caveats: []string{"A session is not guaranteed to equal a user goal or complete outcome."}},
			{Name: "agent", Description: "Observed agent and subagent topology.", Fields: []string{"agentId", "agentDefinition", "agentType", "parentAgentId", "model", "activityCount", "tokens"}, Caveats: []string{"Only reported or safely derived parentage is included."}},
			{Name: "activity", Description: "Bounded chronological evidence.", Fields: []string{"kind", "toolName", "targetAgentId", "status", "startedAt", "endedAt", "traceId", "spanId", "tokens"}, Caveats: []string{"Pages are capped at 100; bodies are opt-in."}},
			{Name: "trace", Description: "Causal trace evidence across participating runs.", Fields: []string{"traceId", "status", "rootSpanCount", "missingParentCount", "conversations", "agents", "activities"}, Caveats: []string{"A trace is not a run identity."}},
			{Name: "tokenUsage", Description: "Observed typed token counters.", Fields: []string{"input", "output", "cacheRead", "cacheWrite", "reasoning", "total"}, Caveats: []string{"null means unavailable; it is not zero."}},
		},
		Tools: []ToolCapability{
			{Name: "list_runs", Purpose: "Discover candidate runs", Optional: []string{"range", "source", "search", "pageSize", "pageToken"}, Returns: []string{"bounded run summaries", "aggregates"}},
			{Name: "get_run_context", Purpose: "Verify one explicit run identity", Required: []string{"source", "runId"}, Returns: []string{"run metadata", "traces", "agent topology"}},
			{Name: "get_run_summary", Purpose: "Measure one run", Required: []string{"source", "runId"}, Returns: []string{"tokens", "agents", "wall duration", "observed active duration", "activity projection completeness", "source coverage"}},
			{Name: "get_run_timeline", Purpose: "Inspect supporting evidence", Required: []string{"source", "runId"}, Optional: []string{"agentId", "pageSize", "pageToken", "direction", "includeContent"}, Returns: []string{"bounded activity page"}},
			{Name: "get_trace", Purpose: "Inspect cross-run causal evidence", Required: []string{"traceId"}, Optional: []string{"pageSize", "pageToken", "includeContent"}, Returns: []string{"trace summary", "participants", "bounded activities"}},
			{Name: "get_token_usage", Purpose: "Compare observed token usage", Required: []string{"source", "runId"}, Returns: []string{"run total", "agent breakdown"}},
			{Name: "find_bottlenecks", Purpose: "Find long observed activities", Required: []string{"source", "runId"}, Returns: []string{"evidence-backed findings"}},
			{Name: "find_coordination_risks", Purpose: "Find explicit coordination evidence", Required: []string{"source", "runId"}, Returns: []string{"error and missing-target findings"}},
			{Name: "analyze_rework", Purpose: "Measure observed development rework", Required: []string{"source", "runId"}, Returns: []string{"validation failures", "fail-fix-retry cycles", "rework duration and tokens", "tool failure rate", "API retry waste", "repeated commands and file edits", "capability limits"}},
			{Name: "compare_runs", Purpose: "Compare explicitly selected runs", Required: []string{"runs"}, Optional: []string{"dimensions"}, Returns: []string{"observed summary metrics"}},
			{Name: "get_source_capabilities", Purpose: "Explain source observability limits", Optional: []string{"source"}, Returns: []string{"available/conditional/unavailable capability matrix"}},
		},
		Workflow:          []string{"Call get_agent_context", "Use list_runs only for discovery", "Establish source-qualified identity with get_run_context", "Call get_run_summary", "Retrieve timeline pages for evidence", "Use analysis tools and report completeness", "Compare only runs with comparable capture conditions"},
		Limits:            []string{"Read-only MCP", "Page size is capped at 100", "Bodies are excluded unless includeContent is true", "Analysis is bounded and may be partial for long runs"},
		UnavailableForNow: []string{"Automatic caller-to-run binding", "Guaranteed task outcome or success criteria", "Complete git diff/commit/test result correlation", "Hidden chain of thought", "Unreported artifact conflicts"},
	}, nil
}

func (service *Service) getRunContext(ctx context.Context, _ *mcp.CallToolRequest, input RunContextInput) (*mcp.CallToolResult, RunContextOutput, error) {
	identity, err := query.NewConversationIdentity(input.Source, input.RunID)
	if err != nil {
		return nil, RunContextOutput{}, err
	}
	summary, err := service.summaryReader.GetSessionSummary(ctx, identity)
	if errors.Is(err, query.ErrConversationNotFound) {
		return nil, RunContextOutput{}, fmt.Errorf("run not found for source %q and runId %q", input.Source, input.RunID)
	}
	if err != nil {
		return nil, RunContextOutput{}, err
	}
	return nil, RunContextOutput{
		Context:  RunContext{SourceID: summary.SourceID, RunID: summary.ID, TraceIDs: append([]string{}, summary.TraceIDs...), Agents: mapAgents(summary.Agents), StartedAt: summary.StartedAt, EndedAt: summary.EndedAt},
		Metadata: completeMetadata("Run identity was explicitly supplied and verified; source events may still be incomplete."),
	}, nil
}

func (service *Service) getRunSummary(ctx context.Context, _ *mcp.CallToolRequest, input RunContextInput) (*mcp.CallToolResult, RunSummaryOutput, error) {
	summary, activities, err := service.loadRunAnalysis(ctx, input)
	if err != nil {
		return nil, RunSummaryOutput{}, err
	}
	efficiency := query.AnalyzeRun(summary, activities)
	return nil, RunSummaryOutput{
		Run:        mapSession(summary),
		Efficiency: EfficiencyOutput{WallDurationMs: efficiency.WallDuration.Milliseconds(), ActiveDurationMs: efficiency.ActiveDuration.Milliseconds(), ParallelismFactor: efficiency.Parallelism, Complete: efficiency.Complete, ActivityCoverage: query.ActivityCoverage(summary, activities)},
		Evidence:   evidenceFromActivities(activities, 20),
		Metadata:   analysisMetadata(query.ActivityCoverage(summary, activities), "Active duration is calculated from returned activity intervals; parallelism is a derived heuristic."),
	}, nil
}

func (service *Service) getTokenUsage(ctx context.Context, _ *mcp.CallToolRequest, input RunContextInput) (*mcp.CallToolResult, TokenUsageDataOutput, error) {
	summary, err := service.loadSummary(ctx, input)
	if err != nil {
		return nil, TokenUsageDataOutput{}, err
	}
	byAgent := make([]TokenAgentOutput, 0, len(summary.Agents))
	for _, agent := range summary.Agents {
		byAgent = append(byAgent, TokenAgentOutput{AgentID: agent.AgentID, AgentDefinition: agent.AgentDefinition, AgentType: agent.AgentType, ParentAgentID: agent.ParentAgentID, Model: agent.Model, ActivityCount: agent.ActivityCount, Tokens: mapTokens(agent.Tokens)})
	}
	return nil, TokenUsageDataOutput{SourceID: summary.SourceID, RunID: summary.ID, Total: mapTokens(summary.Tokens), ByAgent: byAgent, Evidence: summaryEvidence(summary), Metadata: completeMetadata("Token counters are observed from the stored projection; missing components remain null.")}, nil
}

func (service *Service) findBottlenecks(ctx context.Context, _ *mcp.CallToolRequest, input RunContextInput) (*mcp.CallToolResult, FindingsOutput, error) {
	summary, activities, err := service.loadRunAnalysis(ctx, input)
	if err != nil {
		return nil, FindingsOutput{}, err
	}
	return nil, FindingsOutput{SourceID: summary.SourceID, RunID: summary.ID, Findings: mapFindings(query.FindBottlenecks(summary, activities)), Metadata: analysisMetadata(query.ActivityCoverage(summary, activities), "Only observed activity durations are ranked; this is not a critical-path proof.")}, nil
}

func (service *Service) findCoordinationRisks(ctx context.Context, _ *mcp.CallToolRequest, input RunContextInput) (*mcp.CallToolResult, FindingsOutput, error) {
	summary, activities, err := service.loadRunAnalysis(ctx, input)
	if err != nil {
		return nil, FindingsOutput{}, err
	}
	return nil, FindingsOutput{SourceID: summary.SourceID, RunID: summary.ID, Findings: mapFindings(query.FindCoordinationRisks(summary, activities)), Metadata: analysisMetadata(query.ActivityCoverage(summary, activities), "Only explicit error and delegation-target evidence is reported; hidden coordination failures are not inferred.")}, nil
}

func (service *Service) analyzeRework(ctx context.Context, _ *mcp.CallToolRequest, input RunContextInput) (*mcp.CallToolResult, ReworkAnalysisOutput, error) {
	summary, activities, err := service.loadRunAnalysis(ctx, input)
	if err != nil {
		return nil, ReworkAnalysisOutput{}, err
	}
	report := query.AnalyzeRework(summary, activities)
	return nil, ReworkAnalysisOutput{
		SourceID: summary.SourceID,
		RunID:    summary.ID,
		Metrics: ReworkMetricsOutput{
			ValidationFailures: report.ValidationFailures, FailFixRetryCycles: report.FailFixRetryCycles,
			ReworkDurationMs: report.ReworkDuration.Milliseconds(), ReworkTokens: mapTokens(report.ReworkTokens),
			TotalAgentEffortMs: report.TotalAgentEffort.Milliseconds(), ReworkAgentEffortRate: report.ReworkAgentEffortRate,
			ToolAttemptsWithOutcome: report.ToolAttemptsWithOutcome, ToolFailures: report.ToolFailures,
			ToolFailureRate:  report.ToolFailureRate,
			APIRetryWaste:    APIRetryWasteOutput{Attempts: report.APIRetryWaste.Attempts, DurationMs: report.APIRetryWaste.Duration.Milliseconds(), Tokens: mapTokens(report.APIRetryWaste.Tokens)},
			RepeatedCommands: report.RepeatedCommands, ReeditedFiles: report.ReeditedFiles,
			ValidationAttemptsWithOutcome: report.ValidationAttemptsWithOutcome,
			FirstPassEligibleValidations:  report.FirstPassEligibleValidations, FirstPassSuccesses: report.FirstPassSuccesses, FirstPassSuccessRate: report.FirstPassSuccessRate,
			RecurringFailureLoops: report.RecurringFailureLoops, RepeatedFailureAttempts: report.RepeatedFailureAttempts,
			ResolvedFailureLoops: report.ResolvedFailureLoops, UnresolvedFailureLoops: report.UnresolvedFailureLoops,
			FailureResolutionDurationMs: report.FailureResolutionDuration.Milliseconds(), FailureResolutionTokens: mapTokens(report.FailureResolutionTokens),
		},
		Cycles: append([]query.ReworkCycle{}, report.Cycles...), Coverage: report.Coverage,
		FailureEpisodes: append([]query.RecurringFailureEpisode{}, report.FailureEpisodes...),
		Capabilities:    report.Capabilities,
		Metadata:        analysisMetadata(report.Coverage.ActivityCoverage, "Rework is an observed operational proxy. Missing outcomes are excluded; retry matching is heuristic and source attribute coverage may vary."),
	}, nil
}

func (service *Service) compareRuns(ctx context.Context, _ *mcp.CallToolRequest, input CompareRunsInput) (*mcp.CallToolResult, CompareRunsOutput, error) {
	if len(input.Runs) < 1 || len(input.Runs) > 10 {
		return nil, CompareRunsOutput{}, fmt.Errorf("runs must contain between 1 and 10 items")
	}
	dimensions := input.Dimensions
	if len(dimensions) == 0 {
		dimensions = []string{"wallDuration", "activityCount", "agentCount", "totalTokens", "costUsd"}
	}
	allowedDimensions := map[string]struct{}{"wallDuration": {}, "activityCount": {}, "agentCount": {}, "totalTokens": {}, "costUsd": {}}
	for _, dimension := range dimensions {
		if _, ok := allowedDimensions[dimension]; !ok {
			return nil, CompareRunsOutput{}, fmt.Errorf("unsupported comparison dimension %q", dimension)
		}
	}
	output := CompareRunsOutput{Runs: make([]ComparedRunOutput, 0, len(input.Runs)), Dimensions: append([]string{}, dimensions...), Evidence: make([]query.Evidence, 0, len(input.Runs)), Metadata: completeMetadata("Comparison uses explicitly selected run summaries; it does not claim comparable capture conditions.")}
	for _, reference := range input.Runs {
		summary, err := service.loadSummary(ctx, RunContextInput{Source: reference.Source, RunID: reference.RunID})
		if err != nil {
			return nil, CompareRunsOutput{}, err
		}
		var total *int64
		if summary.Tokens.TotalReported() {
			value := summary.Tokens.Total()
			total = &value
		}
		var cost *float64
		if summary.CostUSD != nil {
			value := *summary.CostUSD
			cost = &value
		}
		output.Runs = append(output.Runs, ComparedRunOutput{SourceID: summary.SourceID, RunID: summary.ID, StartedAt: summary.StartedAt, EndedAt: summary.EndedAt, WallDurationMs: summary.EndedAt.Sub(summary.StartedAt).Milliseconds(), ActivityCount: summary.ActivityCount, AgentCount: summary.AgentCount, TotalTokens: total, CostUSD: cost})
		output.Evidence = append(output.Evidence, summaryEvidence(summary)...)
	}
	return nil, output, nil
}

func (service *Service) loadSummary(ctx context.Context, input RunContextInput) (query.Session, error) {
	identity, err := query.NewConversationIdentity(input.Source, input.RunID)
	if err != nil {
		return query.Session{}, err
	}
	summary, err := service.summaryReader.GetSessionSummary(ctx, identity)
	if errors.Is(err, query.ErrConversationNotFound) {
		return query.Session{}, fmt.Errorf("run not found for source %q and runId %q", input.Source, input.RunID)
	}
	return summary, err
}

func (service *Service) loadRunAnalysis(ctx context.Context, input RunContextInput) (query.Session, []query.Activity, error) {
	summary, err := service.loadSummary(ctx, input)
	if err != nil {
		return query.Session{}, nil, err
	}
	identity, err := query.NewConversationIdentity(input.Source, input.RunID)
	if err != nil {
		return query.Session{}, nil, err
	}
	activities := make([]query.Activity, 0, min(int(summary.ActivityCount), 1000))
	for offset := 0; offset < 1000; offset += 100 {
		queryPage, pageErr := query.NewPage(offset, 100)
		if pageErr != nil {
			return query.Session{}, nil, pageErr
		}
		page, pageErr := service.activityReader.ListSessionActivities(ctx, query.ActivityPageFilter{Identity: identity, Page: queryPage, Direction: query.TimelineOlder})
		if errors.Is(pageErr, query.ErrConversationNotFound) {
			break
		}
		if pageErr != nil {
			return query.Session{}, nil, pageErr
		}
		activities = append(activities, page.Activities...)
		if !page.HasMore || len(page.Activities) == 0 {
			break
		}
	}
	return summary, activities, nil
}

func completeMetadata(note string) AnalysisMetadataOutput {
	return AnalysisMetadataOutput{RuleVersion: query.AnalysisRuleVersion, Confidence: "observed", SourceCompleteness: "observed_projection_complete", SourceCoverage: "unknown", RedactionState: "body_not_returned", Note: note}
}

func analysisMetadata(coverage string, note string) AnalysisMetadataOutput {
	return AnalysisMetadataOutput{RuleVersion: query.AnalysisRuleVersion, Confidence: "heuristic", SourceCompleteness: coverage, SourceCoverage: "unknown", RedactionState: "body_not_returned", Note: note}
}

func evidenceFromActivities(activities []query.Activity, limit int) []query.Evidence {
	if len(activities) > limit {
		activities = activities[:limit]
	}
	output := make([]query.Evidence, 0, len(activities))
	for _, activity := range activities {
		output = append(output, query.Evidence{Source: activity.Source, RunID: activity.RunID, TraceID: activity.TraceID, SpanID: activity.SpanID, Name: activity.Name, AgentID: activity.AgentID, Activity: string(activity.Kind)})
	}
	return output
}

func summaryEvidence(summary query.Session) []query.Evidence {
	output := make([]query.Evidence, 0, len(summary.Agents))
	for _, agent := range summary.Agents {
		output = append(output, query.Evidence{Source: summary.SourceID, RunID: summary.ID, AgentID: agent.AgentID, Name: agent.AgentDefinition, Activity: "agent_summary"})
	}
	return output
}

func mapFindings(findings []query.Finding) []FindingOutput {
	output := make([]FindingOutput, 0, len(findings))
	for _, finding := range findings {
		output = append(output, FindingOutput{ID: finding.ID, Kind: finding.Kind, Severity: finding.Severity, Summary: finding.Summary, Metric: finding.Metric, Value: finding.Value, Unit: finding.Unit, Confidence: finding.Confidence, SourceCompleteness: finding.Completeness, Evidence: append([]query.Evidence{}, finding.Evidence...)})
	}
	return output
}

func (service *Service) getSourceCapabilities(_ context.Context, _ *mcp.CallToolRequest, input SourceCapabilitiesInput) (*mcp.CallToolResult, SourceCapabilitiesOutput, error) {
	capabilities := []SourceCapabilityOutput{
		{Source: "claude", Capability: "session_and_agent_identity", State: "available", Description: "Session, agent, parent-agent, and model fields may be available when telemetry is configured.", Evidence: "normalized source profile"},
		{Source: "claude", Capability: "prompt_response_and_tool_body", State: "conditional", Description: "Depends on content capture settings; absence is not an empty body.", Evidence: "source capture policy"},
		{Source: "claude", Capability: "cost", State: "conditional", Description: "Estimated cost can be observed for supported request events.", Evidence: "normalized source profile"},
		{Source: "codex", Capability: "thread_and_agent_relationships", State: "conditional", Description: "Thread/agent relationships depend on exported event attributes and stable source schemas.", Evidence: "normalized source profile"},
		{Source: "codex", Capability: "typed_token_usage", State: "available", Description: "Input, output, cache, and reasoning usage may be observed on response events.", Evidence: "normalized source profile"},
		{Source: "codex", Capability: "prompt_response_and_tool_body", State: "conditional", Description: "Bodies may be absent or encrypted; the MCP does not decrypt source-protected content.", Evidence: "normalized source profile"},
		{Source: "codex", Capability: "cost", State: "unavailable", Description: "Local OTLP does not guarantee an observed cost value.", Evidence: "normalized source profile"},
	}
	if input.Source != "" {
		filtered := capabilities[:0]
		for _, capability := range capabilities {
			if capability.Source == input.Source {
				filtered = append(filtered, capability)
			}
		}
		capabilities = filtered
	}
	return nil, SourceCapabilitiesOutput{Capabilities: capabilities, Metadata: completeMetadata("Capability state describes observability, not execution quality or task success.")}, nil
}

func (service *Service) getTrace(ctx context.Context, _ *mcp.CallToolRequest, input TraceInput) (*mcp.CallToolResult, TraceOutput, error) {
	traceID, err := query.ParseTraceID(input.TraceID)
	if err != nil {
		return nil, TraceOutput{}, err
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, TraceOutput{}, fmt.Errorf("pageSize must be between 1 and 100")
	}
	offset, err := parsePageToken(input.PageToken)
	if err != nil {
		return nil, TraceOutput{}, err
	}
	queryPage, err := query.NewPage(offset, pageSize)
	if err != nil {
		return nil, TraceOutput{}, err
	}
	trace, err := service.traceReader.GetTrace(ctx, query.TraceFilter{TraceID: traceID, Page: queryPage})
	if err != nil {
		return nil, TraceOutput{}, err
	}
	return nil, TraceOutput{Trace: mapTrace(trace, input.IncludeContent)}, nil
}

func (service *Service) getOverview(ctx context.Context, _ *mcp.CallToolRequest, input OverviewInput) (*mcp.CallToolResult, OverviewOutput, error) {
	filter, err := query.FilterForRange(service.now(), input.Range)
	if err != nil {
		return nil, OverviewOutput{}, err
	}
	filter.SourceID, filter.Search = input.Source, input.Search
	dashboard, err := service.dashboardReader.GetDashboard(ctx, query.DashboardFilter{Since: filter.Since, SourceID: input.Source, Search: input.Search})
	if err != nil {
		return nil, OverviewOutput{}, err
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, OverviewOutput{}, fmt.Errorf("pageSize must be between 1 and 100")
	}
	offset, err := parsePageToken(input.PageToken)
	if err != nil {
		return nil, OverviewOutput{}, err
	}
	queryPage, err := query.NewPage(offset, pageSize)
	if err != nil {
		return nil, OverviewOutput{}, err
	}
	sessions, err := service.sessionReader.ListSessions(ctx, query.SessionListFilter{Since: filter.Since, SourceID: input.Source, Search: input.Search, Page: queryPage})
	if err != nil {
		return nil, OverviewOutput{}, err
	}
	output := OverviewOutput{Overview: mapDashboardAndSessions(dashboard, sessions.Sessions)}
	if sessions.HasMore {
		output.NextPageToken = encodePageToken(sessions.NextOffset)
	}
	if offset > 0 {
		output.PreviousPageToken = encodePageToken(max(0, offset-pageSize))
	}
	return nil, output, nil
}

func (service *Service) getSessionActivities(ctx context.Context, _ *mcp.CallToolRequest, input SessionActivitiesInput) (*mcp.CallToolResult, SessionActivitiesOutput, error) {
	if input.Source == "" || input.SessionID == "" {
		return nil, SessionActivitiesOutput{}, fmt.Errorf("source and sessionId are required")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, SessionActivitiesOutput{}, fmt.Errorf("pageSize must be between 1 and 100")
	}
	offset, err := parsePageToken(input.PageToken)
	if err != nil {
		return nil, SessionActivitiesOutput{}, err
	}
	identity, err := query.NewConversationIdentity(input.Source, input.SessionID)
	if err != nil {
		return nil, SessionActivitiesOutput{}, err
	}
	direction, err := query.ParseTimelineDirection(input.Direction)
	if err != nil {
		return nil, SessionActivitiesOutput{}, err
	}
	queryPage, err := query.NewPage(offset, pageSize)
	if err != nil {
		return nil, SessionActivitiesOutput{}, err
	}
	page, err := service.activityReader.ListSessionActivities(ctx, query.ActivityPageFilter{Identity: identity, AgentID: input.AgentID, Page: queryPage, Direction: direction})
	if err != nil {
		return nil, SessionActivitiesOutput{}, err
	}
	output := SessionActivitiesOutput{Source: input.Source, SessionID: input.SessionID, Total: page.Total, HasEarlier: page.HasEarlier, HasMore: page.HasMore, Activities: make([]ActivityOutput, 0, len(page.Activities))}
	for _, activity := range page.Activities {
		output.Activities = append(output.Activities, mapActivityWithContent(activity, input.IncludeContent))
	}
	if page.HasMore {
		output.NextPageToken = encodePageToken(page.Offset + len(page.Activities))
	}
	if page.HasEarlier {
		output.PreviousPageToken = encodePageToken(max(0, page.Offset-pageSize))
	}
	return nil, output, nil
}

func (service *Service) getRunTimeline(ctx context.Context, request *mcp.CallToolRequest, input RunTimelineInput) (*mcp.CallToolResult, SessionActivitiesOutput, error) {
	if input.Direction == "newer" {
		return nil, SessionActivitiesOutput{}, fmt.Errorf("direction newer is not supported; use older")
	}
	offset, err := parseScopedPageToken(input.PageToken, input.Source, input.RunID, "older")
	if err != nil {
		return nil, SessionActivitiesOutput{}, err
	}
	result, output, err := service.getSessionActivities(ctx, request, SessionActivitiesInput{
		Source: input.Source, SessionID: input.RunID, AgentID: input.AgentID, PageSize: input.PageSize,
		PageToken: encodePageToken(offset), Direction: "older", IncludeContent: input.IncludeContent,
	})
	if err != nil {
		return result, output, err
	}
	if output.NextPageToken != "" {
		nextOffset, parseErr := parsePageToken(output.NextPageToken)
		if parseErr != nil {
			return nil, SessionActivitiesOutput{}, parseErr
		}
		output.NextPageToken = encodeScopedPageToken(input.Source, input.RunID, "older", nextOffset)
	}
	if output.PreviousPageToken != "" {
		previousOffset, parseErr := parsePageToken(output.PreviousPageToken)
		if parseErr != nil {
			return nil, SessionActivitiesOutput{}, parseErr
		}
		output.PreviousPageToken = encodeScopedPageToken(input.Source, input.RunID, "older", previousOffset)
	}
	return result, output, nil
}

func parseScopedPageToken(value, source, runID, direction string) (int, error) {
	if value == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, fmt.Errorf("invalid pageToken")
	}
	parts := strings.Split(string(decoded), ":")
	if len(parts) != 6 || parts[0] != "agentmetry" || parts[1] != "v2" || parts[5] == "" {
		return 0, fmt.Errorf("invalid pageToken")
	}
	decodedSource, sourceErr := base64.RawURLEncoding.DecodeString(parts[2])
	decodedRun, runErr := base64.RawURLEncoding.DecodeString(parts[3])
	if sourceErr != nil || runErr != nil || string(decodedSource) != source || string(decodedRun) != runID || parts[4] != direction {
		return 0, fmt.Errorf("pageToken does not match source, runId, or direction")
	}
	offset, err := strconv.Atoi(parts[5])
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid pageToken")
	}
	return offset, nil
}

func encodeScopedPageToken(source, runID, direction string, offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join([]string{
		"agentmetry", "v2", base64.RawURLEncoding.EncodeToString([]byte(source)), base64.RawURLEncoding.EncodeToString([]byte(runID)), direction, strconv.Itoa(offset),
	}, ":")))
}

func parsePageToken(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || !strings.HasPrefix(string(decoded), "agentmetry:v1:") {
		return 0, fmt.Errorf("invalid pageToken")
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(string(decoded), "agentmetry:v1:"))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid pageToken")
	}
	return offset, nil
}

func encodePageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("agentmetry:v1:%d", offset)))
}

func mapDashboardAndSessions(overview query.Overview, sessions []query.Session) OverviewDataOutput {
	output := mapOverview(overview)
	output.Sessions = make([]SessionOutput, 0, len(sessions))
	for _, session := range sessions {
		output.Sessions = append(output.Sessions, mapSessionSummary(session))
	}
	return output
}

func mapSessionSummary(session query.Session) SessionOutput {
	return mapSession(session)
}

func mapSession(session query.Session) SessionOutput {
	return SessionOutput{
		ID: session.ID, SourceID: session.SourceID, Sources: session.Sources, TraceIDs: session.TraceIDs,
		StartedAt: session.StartedAt, EndedAt: session.EndedAt, ActivityCount: session.ActivityCount,
		Tokens: mapTokens(session.Tokens), CostUSD: session.CostUSD,
		Agents: mapAgents(session.Agents), Activities: make([]ActivityOutput, 0),
	}
}

func mapAgents(agents []query.AgentSession) []AgentSessionOutput {
	output := make([]AgentSessionOutput, 0, len(agents))
	for _, agent := range agents {
		output = append(output, AgentSessionOutput{AgentID: agent.AgentID, AgentDefinition: agent.AgentDefinition, AgentType: agent.AgentType, ParentAgentID: agent.ParentAgentID, Model: agent.Model, ActivityCount: agent.ActivityCount, Tokens: mapTokens(agent.Tokens)})
	}
	return output
}

func mapOverview(overview query.Overview) OverviewDataOutput {
	output := OverviewDataOutput{
		Sources: overview.Sources,
		SignalCounts: SignalCountsOutput{
			Traces: overview.SignalCounts.Traces, Logs: overview.SignalCounts.Logs, Metrics: overview.SignalCounts.Metrics,
		},
		RunCount: overview.RunCount, AgentCount: overview.AgentCount, Tokens: mapTokens(overview.Tokens),
		PlanUsage: overview.PlanUsage,
	}
	for _, activity := range overview.RecentActivity {
		output.RecentActivity = append(output.RecentActivity, mapActivity(activity))
	}
	for _, session := range overview.Sessions {
		mapped := SessionOutput{
			ID: session.ID, SourceID: session.SourceID, Sources: session.Sources, TraceIDs: session.TraceIDs, StartedAt: session.StartedAt, EndedAt: session.EndedAt,
			ActivityCount: session.ActivityCount, Tokens: mapTokens(session.Tokens), CostUSD: session.CostUSD,
		}
		for _, agent := range session.Agents {
			mapped.Agents = append(mapped.Agents, AgentSessionOutput{
				AgentID: agent.AgentID, AgentDefinition: agent.AgentDefinition, AgentType: agent.AgentType,
				ParentAgentID: agent.ParentAgentID, Model: agent.Model,
				ActivityCount: agent.ActivityCount, Tokens: mapTokens(agent.Tokens),
			})
		}
		for _, activity := range session.Activities {
			mapped.Activities = append(mapped.Activities, mapActivity(activity))
		}
		output.Sessions = append(output.Sessions, mapped)
	}
	return output
}

func mapTrace(trace query.Trace, includeContent bool) TraceDataOutput {
	output := TraceDataOutput{
		TraceID: trace.TraceID, StartedAt: trace.StartedAt, EndedAt: trace.EndedAt, Status: string(trace.Status),
		RootSpanCount: trace.RootSpanCount, MissingParentCount: trace.MissingParentCount,
		Conversations:  append([]query.ConversationRef{}, trace.Conversations...),
		Agents:         append([]query.TraceAgent{}, trace.Agents...),
		Activities:     make([]ActivityOutput, 0, len(trace.Activities)),
		ActivityOffset: trace.ActivityOffset, ActivityCount: trace.ActivityCount, HasMore: trace.HasMore,
	}
	if trace.HasMore {
		output.NextPageToken = encodePageToken(trace.ActivityOffset + len(trace.Activities))
	}
	if trace.ActivityOffset > 0 {
		output.PreviousPageToken = encodePageToken(max(0, trace.ActivityOffset-len(trace.Activities)))
	}
	for _, activity := range trace.Activities {
		output.Activities = append(output.Activities, mapActivityWithContent(activity, includeContent))
	}
	return output
}

func mapActivity(activity query.Activity) ActivityOutput {
	return mapActivityWithContent(activity, false)
}

func mapActivityWithContent(activity query.Activity, includeContent bool) ActivityOutput {
	return ActivityOutput{
		Source: activity.Source, Signal: string(activity.Signal), TraceID: activity.TraceID, SpanID: activity.SpanID,
		ParentSpanID: activity.ParentSpanID, Name: activity.Name, Kind: string(activity.Kind),
		ToolName: activity.ToolName, TargetAgentID: activity.TargetAgentID, TargetAgentType: activity.TargetAgentType, Content: content(activity.Content, includeContent), ContentState: contentState(activity.Content, includeContent),
		AgentID: activity.AgentID, AgentDefinition: activity.AgentDefinition,
		AgentType: activity.AgentType, ParentAgentID: activity.ParentAgentID,
		RunID: activity.RunID, Model: activity.Model, StartedAt: activity.StartedAt, EndedAt: activity.EndedAt,
		ObservedAt: activity.ObservedAt, Status: activity.Status,
		Tokens: mapTokens(activity.Tokens), CostUSD: activity.CostUSD, ContributesToTotal: activity.ContributesToTotal,
	}
}

func content(value string, include bool) string {
	if !include {
		return ""
	}
	return value
}

func contentState(value string, include bool) string {
	if !include {
		return "not_returned"
	}
	if value == "" {
		return "unavailable"
	}
	return "available"
}

func mapTokens(tokens canonical.TokenUsage) TokenUsageOutput {
	total := tokens.Total()
	return TokenUsageOutput{
		Input:      reportedToken(tokens.Input, tokens.InputReported()),
		Output:     reportedToken(tokens.Output, tokens.OutputReported()),
		CacheRead:  reportedToken(tokens.CacheRead, tokens.CacheReadReported()),
		CacheWrite: reportedToken(tokens.CacheWrite, tokens.CacheWriteReported()),
		Reasoning:  reportedToken(tokens.Reasoning, tokens.ReasoningReported()),
		Total:      reportedToken(total, tokens.TotalReported()),
	}
}

func reportedToken(value int64, reported bool) *int64 {
	if !reported {
		return nil
	}
	return &value
}
