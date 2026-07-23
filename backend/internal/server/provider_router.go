package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

const (
	providerFailureThreshold = 3
	providerCircuitCooldown  = 30 * time.Second
)

type providerGroup string

const (
	providerGroupCodex    providerGroup = providerGroup(textProfileClientCodex)
	providerGroupClaude   providerGroup = providerGroup(textProfileClientClaude)
	providerGroupOpenCode providerGroup = providerGroup(textProfileClientOpenCode)
)

var providerGroups = []providerGroup{providerGroupCodex, providerGroupClaude, providerGroupOpenCode}

type providerRouteContextKey struct{}
type providerRouteTraceContextKey struct{}

type providerRouteRequest struct {
	once       sync.Once
	group      providerGroup
	candidate  providerRouteCandidate
	configured bool
}

type providerRouteTrace struct {
	mu       sync.RWMutex
	selected providerRouteSelection
}

type providerRouteSelection struct {
	Group     providerGroup
	ProfileID string
	Name      string
	Provider  string
	Model     string
}

func withProviderRouteContext(ctx context.Context, group providerGroup) context.Context {
	if ctx == nil || !group.valid() {
		return ctx
	}
	ctx = context.WithValue(ctx, providerRouteContextKey{}, &providerRouteRequest{group: group})
	return context.WithValue(ctx, providerRouteTraceContextKey{}, &providerRouteTrace{})
}

func providerGroupFromContext(ctx context.Context) (providerGroup, bool) {
	if ctx == nil {
		return "", false
	}
	route, ok := ctx.Value(providerRouteContextKey{}).(*providerRouteRequest)
	if !ok || route == nil || !route.group.valid() {
		return "", false
	}
	return route.group, true
}

func providerRouteRequestFromContext(ctx context.Context) *providerRouteRequest {
	if ctx == nil {
		return nil
	}
	route, _ := ctx.Value(providerRouteContextKey{}).(*providerRouteRequest)
	return route
}

func (a *app) resolveProviderRoute(ctx context.Context, primary endpoint) (providerRouteCandidate, bool) {
	route := providerRouteRequestFromContext(ctx)
	if route == nil || !route.group.valid() {
		return providerRouteCandidate{}, false
	}
	route.once.Do(func() {
		route.candidate, route.configured = providerRouteCandidateForGroup(a.currentConfig(), route.group, primary)
	})
	return route.candidate, route.configured
}

func providerRouteTraceFromContext(ctx context.Context) *providerRouteTrace {
	if ctx == nil {
		return nil
	}
	trace, _ := ctx.Value(providerRouteTraceContextKey{}).(*providerRouteTrace)
	return trace
}

func (t *providerRouteTrace) set(selection providerRouteSelection) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.selected = selection
	t.mu.Unlock()
}

func (t *providerRouteTrace) get() (providerRouteSelection, bool) {
	if t == nil {
		return providerRouteSelection{}, false
	}
	t.mu.RLock()
	selection := t.selected
	t.mu.RUnlock()
	return selection, selection.ProfileID != ""
}

func providerGroupForClient(client string) (providerGroup, bool) {
	normalized, ok := normalizeTextProfileClientID(client)
	if !ok {
		return "", false
	}
	group := providerGroup(normalized)
	return group, group.valid()
}

func (g providerGroup) valid() bool {
	switch g {
	case providerGroupCodex, providerGroupClaude, providerGroupOpenCode:
		return true
	default:
		return false
	}
}

type providerCircuitState string

const (
	providerCircuitClosed   providerCircuitState = "closed"
	providerCircuitOpen     providerCircuitState = "open"
	providerCircuitHalfOpen providerCircuitState = "half_open"
)

type providerRuntimeState struct {
	FailureCount        int64
	ConsecutiveFailures int
	CircuitState        providerCircuitState
	OpenUntil           time.Time
	HalfOpenInFlight    bool
	LastError           string
	LastFailureAt       time.Time
	LastSuccessAt       time.Time
}

// providerObservedBody delays a successful circuit result until the upstream
// response body has actually completed. Receiving a 2xx response header alone
// is not sufficient for streaming and other long responses.
type providerObservedBody struct {
	ctx       context.Context
	body      io.ReadCloser
	router    *providerRouter
	candidate providerRouteCandidate
	once      sync.Once
}

func newProviderObservedBody(ctx context.Context, body io.ReadCloser, router *providerRouter, candidate providerRouteCandidate) io.ReadCloser {
	return &providerObservedBody{ctx: ctx, body: body, router: router, candidate: candidate}
}

func (b *providerObservedBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if errors.Is(err, io.EOF) {
		b.finish(nil)
	} else if err != nil {
		b.finish(err)
	}
	return n, err
}

func (b *providerObservedBody) Close() error {
	err := b.body.Close()
	b.once.Do(func() {
		// A caller may intentionally stop consuming a response (for example before
		// a protocol fallback). Do not call that an upstream success or failure, but
		// make sure a half-open probe is not left permanently in flight.
		b.router.releaseHalfOpenProbe(b.candidate)
	})
	return err
}

func (b *providerObservedBody) finish(readErr error) {
	b.once.Do(func() {
		if readErr == nil {
			b.router.recordSuccess(b.candidate)
			return
		}
		if shouldRecordProviderFailure(b.ctx, nil, readErr) {
			b.router.recordFailure(b.candidate, readErr)
			return
		}
		b.router.releaseHalfOpenProbe(b.candidate)
	})
}

type providerGroupRuntime struct {
	Providers      map[string]*providerRuntimeState
	LastSelectedID string
	LastSelectedAt time.Time
}

type providerRouter struct {
	mu     sync.Mutex
	groups map[providerGroup]*providerGroupRuntime
	now    func() time.Time
}

func newProviderRouter() *providerRouter {
	return &providerRouter{groups: map[providerGroup]*providerGroupRuntime{}, now: time.Now}
}

func (a *app) textProviderRouter() *providerRouter {
	a.providerRouterMu.Lock()
	defer a.providerRouterMu.Unlock()
	if a.providerRouter == nil {
		a.providerRouter = newProviderRouter()
	}
	return a.providerRouter
}

type providerRouteCandidate struct {
	Group     providerGroup
	ProfileID string
	Name      string
	Config    config
	Endpoint  endpoint
	halfOpen  bool
}

func providerRouteCandidateForGroup(cfg config, group providerGroup, primary endpoint) (providerRouteCandidate, bool) {
	profiles := normalizeTextProfiles(cfg.TextModelProfiles)
	active := normalizeActiveTextProfilesByClient(profiles, cfg.ActiveTextProfileByClient, cfg.ActiveTextProfileID)[string(group)]
	for _, profile := range profiles {
		if profile.Client != string(group) || profile.ID != active {
			continue
		}
		candidateCfg := applyTextProfileToConfig(cfg, profile)
		candidateCfg.ActiveTextProfileID = profile.ID
		return providerRouteCandidate{
			Group:     group,
			ProfileID: profile.ID,
			Name:      profile.Name,
			Config:    candidateCfg,
			Endpoint:  (&app{}).textEndpoint(candidateCfg),
		}, true
	}
	if cfg.legacyTextRouting || len(cfg.TextModelProfiles) == 0 {
		// Preserve only genuine configurations that predate client groups. The
		// runtime state remains namespaced by group, so failures never cross them.
		return providerRouteCandidate{
			Group: group, ProfileID: "legacy-" + string(group), Name: "\u5f53\u524d\u6587\u672c\u4e0a\u6e38",
			Config: cfg, Endpoint: primary,
		}, true
	}
	return providerRouteCandidate{Group: group}, false
}

func (r *providerRouter) selectCandidate(candidate providerRouteCandidate) (providerRouteCandidate, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	state := r.providerStateLocked(candidate.Group, candidate.ProfileID)
	if state.CircuitState == providerCircuitOpen {
		if state.OpenUntil.IsZero() || now.Before(state.OpenUntil) || state.HalfOpenInFlight {
			return candidate, false
		}
		state.CircuitState = providerCircuitHalfOpen
		state.HalfOpenInFlight = true
		candidate.halfOpen = true
	}
	if state.CircuitState == providerCircuitHalfOpen && !candidate.halfOpen {
		if state.HalfOpenInFlight {
			return candidate, false
		}
		state.HalfOpenInFlight = true
		candidate.halfOpen = true
	}
	return candidate, true
}

func (r *providerRouter) providerStateLocked(group providerGroup, profileID string) *providerRuntimeState {
	groupState := r.groups[group]
	if groupState == nil {
		groupState = &providerGroupRuntime{Providers: map[string]*providerRuntimeState{}}
		r.groups[group] = groupState
	}
	state := groupState.Providers[profileID]
	if state == nil {
		state = &providerRuntimeState{CircuitState: providerCircuitClosed}
		groupState.Providers[profileID] = state
	}
	return state
}

func (r *providerRouter) recordSelection(candidate providerRouteCandidate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	group := r.groups[candidate.Group]
	if group == nil {
		group = &providerGroupRuntime{Providers: map[string]*providerRuntimeState{}}
		r.groups[candidate.Group] = group
	}
	group.LastSelectedID = candidate.ProfileID
	group.LastSelectedAt = r.now()
}

func (r *providerRouter) recordSuccess(candidate providerRouteCandidate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.providerStateLocked(candidate.Group, candidate.ProfileID)
	state.ConsecutiveFailures = 0
	state.CircuitState = providerCircuitClosed
	state.OpenUntil = time.Time{}
	state.HalfOpenInFlight = false
	state.LastError = ""
	state.LastSuccessAt = r.now()
}

func (r *providerRouter) releaseHalfOpenProbe(candidate providerRouteCandidate) {
	if !candidate.halfOpen {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.providerStateLocked(candidate.Group, candidate.ProfileID)
	state.HalfOpenInFlight = false
}

func (r *providerRouter) recordFailure(candidate providerRouteCandidate, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.providerStateLocked(candidate.Group, candidate.ProfileID)
	state.FailureCount++
	state.ConsecutiveFailures++
	state.HalfOpenInFlight = false
	state.LastFailureAt = r.now()
	if err != nil {
		state.LastError = err.Error()
	}
	if state.ConsecutiveFailures >= providerFailureThreshold || state.CircuitState == providerCircuitHalfOpen || candidate.halfOpen {
		state.CircuitState = providerCircuitOpen
		state.OpenUntil = state.LastFailureAt.Add(providerCircuitCooldown)
	} else {
		state.CircuitState = providerCircuitClosed
	}
}

func shouldRecordProviderFailure(ctx context.Context, resp *http.Response, err error) bool {
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if err != nil {
		return true
	}
	return resp != nil && resp.StatusCode >= http.StatusInternalServerError
}

func providerAttemptError(resp *http.Response, err error) error {
	if err != nil {
		return err
	}
	if resp == nil {
		return errors.New("upstream returned no response")
	}
	return fmt.Errorf("upstream returned %s", resp.Status)
}

func adaptProviderAttempt(candidate providerRouteCandidate, requestURI string, body []byte) (string, []byte) {
	adaptedURI := geminiRequestURIWithEffectiveModel(candidate.Config, requestURI)
	if len(body) == 0 {
		return adaptedURI, body
	}
	payload := decodeJSONMap(body)
	if payload == nil {
		return adaptedURI, body
	}
	requested := firstString(payload["model"])
	if requested == "" {
		return adaptedURI, body
	}
	if model := effectiveTextModel(candidate.Config, requested); model != "" && model != requested {
		payload["model"] = model
		if encoded, err := jsonMarshal(payload); err == nil {
			return adaptedURI, encoded
		}
	}
	return adaptedURI, body
}

// jsonMarshal is a small seam used by provider routing without changing the
// existing handlers' payload ownership.
var jsonMarshal = func(value any) ([]byte, error) {
	return json.Marshal(value)
}

type providerStatusResponse struct {
	Groups []providerGroupStatus `json:"groups"`
}

type providerGroupStatus struct {
	Group            string                   `json:"group"`
	ActiveProviderID string                   `json:"active_provider_id"`
	LastSelectedID   string                   `json:"last_selected_id,omitempty"`
	Providers        []providerEndpointStatus `json:"providers"`
}

type providerEndpointStatus struct {
	ProfileID          string               `json:"profile_id"`
	Name               string               `json:"name"`
	Provider           string               `json:"provider"`
	BaseURL            string               `json:"base_url"`
	Priority           int                  `json:"priority"`
	Active             bool                 `json:"active"`
	FailureCount       int64                `json:"failure_count"`
	ConsecutiveFailure int                  `json:"consecutive_failures"`
	CircuitState       providerCircuitState `json:"circuit_state"`
	OpenUntil          *time.Time           `json:"open_until,omitempty"`
	LastError          string               `json:"last_error,omitempty"`
	LastFailureAt      *time.Time           `json:"last_failure_at,omitempty"`
	LastSuccessAt      *time.Time           `json:"last_success_at,omitempty"`
}

func (a *app) providerRouterStatus() providerStatusResponse {
	cfg := a.currentConfig()
	router := a.textProviderRouter()
	router.mu.Lock()
	defer router.mu.Unlock()
	response := providerStatusResponse{Groups: make([]providerGroupStatus, 0, len(providerGroups))}
	active := normalizeActiveTextProfilesByClient(cfg.TextModelProfiles, cfg.ActiveTextProfileByClient, cfg.ActiveTextProfileID)
	for _, group := range providerGroups {
		groupStatus := providerGroupStatus{Group: string(group), ActiveProviderID: active[string(group)]}
		if runtime := router.groups[group]; runtime != nil {
			groupStatus.LastSelectedID = runtime.LastSelectedID
		}
		priority := 0
		for _, profile := range normalizeTextProfiles(cfg.TextModelProfiles) {
			if profile.Client != string(group) {
				continue
			}
			priority++
			status := providerEndpointStatus{
				ProfileID:    profile.ID,
				Name:         profile.Name,
				Provider:     profile.Provider,
				BaseURL:      profile.BaseURL,
				Priority:     priority,
				Active:       profile.ID == active[string(group)],
				CircuitState: providerCircuitClosed,
			}
			if runtime := router.groups[group]; runtime != nil {
				if state := runtime.Providers[profile.ID]; state != nil {
					status.FailureCount = state.FailureCount
					status.ConsecutiveFailure = state.ConsecutiveFailures
					status.CircuitState = state.CircuitState
					status.LastError = state.LastError
					status.OpenUntil = timePtrOrNil(state.OpenUntil)
					status.LastFailureAt = timePtrOrNil(state.LastFailureAt)
					status.LastSuccessAt = timePtrOrNil(state.LastSuccessAt)
				}
			}
			groupStatus.Providers = append(groupStatus.Providers, status)
		}
		sort.SliceStable(groupStatus.Providers, func(i, j int) bool { return groupStatus.Providers[i].Priority < groupStatus.Providers[j].Priority })
		response.Groups = append(response.Groups, groupStatus)
	}
	return response
}

func timePtrOrNil(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

func (a *app) handleProviderRouterStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, a.providerRouterStatus())
}
