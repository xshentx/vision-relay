package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	providerFailureThreshold          = 5
	providerCircuitCooldown           = 30 * time.Second
	providerRecoveryProbePollInterval = time.Second
	providerRecoveryProbeTimeout      = 20 * time.Second
	providerRecoveryProbeMaxBodyBytes = 1 << 20
)

type providerGroup string

const (
	providerGroupCodex    providerGroup = providerGroup(textProfileClientCodex)
	providerGroupClaude   providerGroup = providerGroup(textProfileClientClaude)
	providerGroupOpenCode providerGroup = providerGroup(textProfileClientOpenCode)
	providerGroupOpenClaw providerGroup = providerGroup(textProfileClientOpenClaw)
)

var providerGroups = []providerGroup{providerGroupCodex, providerGroupClaude, providerGroupOpenCode, providerGroupOpenClaw}

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
	case providerGroupCodex, providerGroupClaude, providerGroupOpenCode, providerGroupOpenClaw:
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
	FailureCount            int64
	ConsecutiveFailures     int
	CircuitState            providerCircuitState
	OpenUntil               time.Time
	HalfOpenInFlight        bool
	RecoveryProbeCancel     context.CancelFunc
	RecoveryProbeGeneration uint64
	LastRequestedModel      string
	LastError               string
	LastFailureAt           time.Time
	LastSuccessAt           time.Time
}

// providerObservedBody delays a successful circuit result until the upstream
// response body has actually completed. Receiving a 2xx response header alone
// is not sufficient for streaming and other long responses.
type providerObservedBody struct {
	ctx               context.Context
	body              io.ReadCloser
	router            *providerRouter
	candidate         providerRouteCandidate
	once              sync.Once
	validationMu      sync.Mutex
	validationPending bool
	pendingEOF        bool
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
		// A caller may intentionally stop consuming a response before reading it to EOF.
		// Do not call that an upstream success or failure, but
		// make sure a half-open probe is not left permanently in flight.
		b.router.releaseHalfOpenProbe(b.candidate)
	})
	return err
}

func (b *providerObservedBody) reportProviderFailure(err error) {
	b.validationMu.Lock()
	b.validationPending = false
	b.pendingEOF = false
	b.validationMu.Unlock()
	b.finish(err)
}

func (b *providerObservedBody) beginProtocolValidation() {
	b.validationMu.Lock()
	b.validationPending = true
	b.validationMu.Unlock()
}

func (b *providerObservedBody) acceptProtocolValidation() {
	b.validationMu.Lock()
	pendingEOF := b.validationPending && b.pendingEOF
	b.validationPending = false
	b.pendingEOF = false
	b.validationMu.Unlock()
	if pendingEOF {
		b.finish(nil)
	}
}

func (b *providerObservedBody) finish(readErr error) {
	if readErr == nil {
		b.validationMu.Lock()
		if b.validationPending {
			b.pendingEOF = true
			b.validationMu.Unlock()
			return
		}
		b.validationMu.Unlock()
	}
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
	Group                   providerGroup
	ProfileID               string
	Name                    string
	Config                  config
	Endpoint                endpoint
	halfOpen                bool
	recoveryProbeGeneration uint64
	recoveryProbeModel      string
}

func providerRouteCandidateForGroup(cfg config, group providerGroup, primary endpoint) (providerRouteCandidate, bool) {
	profiles := normalizeTextProfiles(cfg.TextModelProfiles)
	active := normalizeActiveTextProfilesByClient(profiles, cfg.ActiveTextProfileByClient, cfg.ActiveTextProfileID)[string(group)]
	for _, profile := range profiles {
		if profile.Client != string(group) || profile.ID != active {
			continue
		}
		return providerRouteCandidateForProfile(cfg, group, profile), true
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

func providerRouteCandidateForProfile(cfg config, group providerGroup, profile textModelProfile) providerRouteCandidate {
	candidateCfg := applyTextProfileToConfig(cfg, profile)
	candidateCfg.ActiveTextProfileID = profile.ID
	return providerRouteCandidate{
		Group:     group,
		ProfileID: profile.ID,
		Name:      profile.Name,
		Config:    candidateCfg,
		Endpoint:  (&app{}).textEndpoint(candidateCfg),
	}
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

// claimDueRecoveryProbes moves only due open circuits into half-open state.
// Closed providers are intentionally excluded so periodic recovery checks never
// create background traffic during normal operation.
func (r *providerRouter) claimDueRecoveryProbes(cfg config) []providerRouteCandidate {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	var due []providerRouteCandidate
	for _, candidate := range providerRecoveryCandidates(cfg) {
		groupRuntime := r.groups[candidate.Group]
		if groupRuntime == nil {
			continue
		}
		state := groupRuntime.Providers[candidate.ProfileID]
		if state == nil || state.CircuitState != providerCircuitOpen || state.HalfOpenInFlight {
			continue
		}
		if !state.OpenUntil.IsZero() && now.Before(state.OpenUntil) {
			continue
		}
		model := strings.TrimSpace(state.LastRequestedModel)
		if model == "" {
			var err error
			model, err = resolveModelTestModel(recoveryProbeProfile(candidate), "")
			if err != nil {
				// A provider without model mappings can still route arbitrary models.
				// Wait until a routed request supplies one instead of treating a
				// local inability to construct a probe as another provider failure.
				continue
			}
		}
		state.CircuitState = providerCircuitHalfOpen
		state.HalfOpenInFlight = true
		state.RecoveryProbeGeneration++
		candidate.halfOpen = true
		candidate.recoveryProbeGeneration = state.RecoveryProbeGeneration
		candidate.recoveryProbeModel = model
		due = append(due, candidate)
	}
	return due
}

// providerRecoveryCandidates includes every explicit supplier plus the
// synthetic per-client candidates used by legacy global routing. Explicit
// inactive suppliers remain eligible because they may retain an open circuit
// after the user switches the active supplier.
func providerRecoveryCandidates(cfg config) []providerRouteCandidate {
	candidates := make([]providerRouteCandidate, 0, len(cfg.TextModelProfiles)+len(providerGroups))
	seen := make(map[providerProfileRuntimeKey]bool)
	for _, profile := range normalizeTextProfiles(cfg.TextModelProfiles) {
		group, ok := providerGroupForClient(profile.Client)
		if !ok {
			continue
		}
		candidate := providerRouteCandidateForProfile(cfg, group, profile)
		key := providerProfileRuntimeKey{Group: group, ProfileID: profile.ID}
		seen[key] = true
		candidates = append(candidates, candidate)
	}
	if !cfg.legacyTextRouting && !cfg.LegacyTextRouting && len(cfg.TextModelProfiles) != 0 {
		return candidates
	}
	primary := (&app{}).textEndpoint(cfg)
	for _, group := range providerGroups {
		candidate, configured := providerRouteCandidateForGroup(cfg, group, primary)
		if !configured {
			continue
		}
		key := providerProfileRuntimeKey{Group: candidate.Group, ProfileID: candidate.ProfileID}
		if seen[key] {
			continue
		}
		seen[key] = true
		candidates = append(candidates, candidate)
	}
	return candidates
}

func recoveryProbeProfile(candidate providerRouteCandidate) textModelProfile {
	if profile, ok := findTextModelProfile(candidate.Config, candidate.ProfileID); ok {
		return normalizeTextProfiles([]textModelProfile{profile})[0]
	}
	profile := textProfileFromConfig(candidate.Config, candidate.ProfileID, candidate.Name)
	profile.Client = string(candidate.Group)
	return profile
}

type providerProfileRuntimeKey struct {
	Group     providerGroup
	ProfileID string
}

func providerRuntimeProfiles(cfg config) map[providerProfileRuntimeKey]textModelProfile {
	profiles := make(map[providerProfileRuntimeKey]textModelProfile)
	for _, candidate := range providerRecoveryCandidates(cfg) {
		profiles[providerProfileRuntimeKey{Group: candidate.Group, ProfileID: candidate.ProfileID}] = recoveryProbeProfile(candidate)
	}
	return profiles
}

func sameProviderRuntimeConfig(left, right textModelProfile) bool {
	// Display-name changes do not affect routing or a recovery probe.
	left.Name = ""
	right.Name = ""
	return reflect.DeepEqual(left, right)
}

// reconcileConfigChange prevents a probe built from a stale endpoint, API key,
// proxy, protocol, or model configuration from changing the replacement
// provider's circuit state. Cancellation happens after invalidation so a late
// transport result is harmless even when it does not stop promptly.
func (r *providerRouter) reconcileConfigChange(previous, current config) {
	previousProfiles := providerRuntimeProfiles(previous)
	currentProfiles := providerRuntimeProfiles(current)
	var cancels []context.CancelFunc

	r.mu.Lock()
	for key, previousProfile := range previousProfiles {
		currentProfile, exists := currentProfiles[key]
		if exists && sameProviderRuntimeConfig(previousProfile, currentProfile) {
			continue
		}
		groupRuntime := r.groups[key.Group]
		if groupRuntime == nil {
			continue
		}
		state := groupRuntime.Providers[key.ProfileID]
		if state == nil {
			continue
		}
		if state.RecoveryProbeCancel != nil {
			cancels = append(cancels, state.RecoveryProbeCancel)
		}
		generation := state.RecoveryProbeGeneration + 1
		*state = providerRuntimeState{
			CircuitState:            providerCircuitClosed,
			RecoveryProbeGeneration: generation,
		}
	}
	r.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

// attachRecoveryProbe registers the cancellation function only while the
// claimed probe still owns the provider's current half-open generation. A
// successful request can close the circuit between claiming and attaching;
// in that case the caller must not start the obsolete probe.
func (r *providerRouter) attachRecoveryProbe(candidate providerRouteCandidate, cancel context.CancelFunc) bool {
	if candidate.recoveryProbeGeneration == 0 || cancel == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.providerStateLocked(candidate.Group, candidate.ProfileID)
	if state.CircuitState != providerCircuitHalfOpen || !state.HalfOpenInFlight ||
		state.RecoveryProbeGeneration != candidate.recoveryProbeGeneration || state.RecoveryProbeCancel != nil {
		return false
	}
	state.RecoveryProbeCancel = cancel
	return true
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

func (r *providerRouter) recordRequestedModel(candidate providerRouteCandidate, requestURI string, body []byte) {
	requested := ""
	if payload := decodeJSONMap(body); payload != nil {
		requested = strings.TrimSpace(firstString(payload["model"]))
	}
	if requested == "" {
		requested = strings.TrimSpace(geminiRequestedModel(requestURI))
	}
	if requested == "" {
		return
	}
	if effective := effectiveTextModel(candidate.Config, requested); effective != "" {
		requested = effective
	}
	r.mu.Lock()
	state := r.providerStateLocked(candidate.Group, candidate.ProfileID)
	state.LastRequestedModel = requested
	r.mu.Unlock()
}

func (r *providerRouter) recordSuccess(candidate providerRouteCandidate) {
	r.mu.Lock()
	state := r.providerStateLocked(candidate.Group, candidate.ProfileID)
	if candidate.recoveryProbeGeneration != 0 && state.RecoveryProbeGeneration != candidate.recoveryProbeGeneration {
		r.mu.Unlock()
		return
	}
	cancelRecoveryProbe := state.RecoveryProbeCancel
	state.RecoveryProbeCancel = nil
	// Invalidate the result of any recovery probe that may not stop promptly
	// after cancellation. This also makes a completed probe idempotent.
	state.RecoveryProbeGeneration++
	state.ConsecutiveFailures = 0
	state.CircuitState = providerCircuitClosed
	state.OpenUntil = time.Time{}
	state.HalfOpenInFlight = false
	state.LastError = ""
	state.LastSuccessAt = r.now()
	r.mu.Unlock()
	if cancelRecoveryProbe != nil {
		cancelRecoveryProbe()
	}
}

func (r *providerRouter) releaseHalfOpenProbe(candidate providerRouteCandidate) {
	if !candidate.halfOpen {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.providerStateLocked(candidate.Group, candidate.ProfileID)
	if candidate.recoveryProbeGeneration != 0 && state.RecoveryProbeGeneration != candidate.recoveryProbeGeneration {
		return
	}
	if candidate.recoveryProbeGeneration != 0 {
		state.RecoveryProbeCancel = nil
	}
	state.HalfOpenInFlight = false
}

func (r *providerRouter) recordFailure(candidate providerRouteCandidate, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.providerStateLocked(candidate.Group, candidate.ProfileID)
	if candidate.recoveryProbeGeneration != 0 && state.RecoveryProbeGeneration != candidate.recoveryProbeGeneration {
		return
	}
	if candidate.recoveryProbeGeneration != 0 {
		state.RecoveryProbeCancel = nil
		state.RecoveryProbeGeneration++
	}
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

func runProviderRecoveryProbes(ctx context.Context, a *app) {
	if ctx == nil || a == nil {
		return
	}
	ticker := time.NewTicker(providerRecoveryProbePollInterval)
	defer ticker.Stop()

	var probes sync.WaitGroup
	defer probes.Wait()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			router := a.textProviderRouter()
			candidates := router.claimDueRecoveryProbes(a.currentConfig())
			for _, candidate := range candidates {
				probeContext, cancel := context.WithCancel(ctx)
				if !router.attachRecoveryProbe(candidate, cancel) {
					cancel()
					continue
				}
				probes.Add(1)
				go func(candidate providerRouteCandidate, probeContext context.Context, cancel context.CancelFunc) {
					defer probes.Done()
					defer cancel()
					a.probeProviderRecovery(probeContext, candidate)
				}(candidate, probeContext, cancel)
			}
		}
	}
}

func (a *app) probeProviderRecovery(parent context.Context, candidate providerRouteCandidate) {
	router := a.textProviderRouter()
	recordFailure := func(err error) {
		if parent.Err() != nil {
			router.releaseHalfOpenProbe(candidate)
			return
		}
		router.recordFailure(candidate, err)
	}
	if parent.Err() != nil {
		router.releaseHalfOpenProbe(candidate)
		return
	}
	profile := recoveryProbeProfile(candidate)
	model := strings.TrimSpace(candidate.recoveryProbeModel)
	if model == "" {
		recordFailure(errors.New("provider recovery probe has no model"))
		return
	}
	spec, err := buildModelTestSpec(profile, model, "hi")
	if err != nil {
		recordFailure(fmt.Errorf("provider recovery probe: %w", err))
		return
	}
	body, err := json.Marshal(spec.Payload)
	if err != nil {
		recordFailure(fmt.Errorf("provider recovery probe: %w", err))
		return
	}

	ctx, cancel := context.WithTimeout(parent, providerRecoveryProbeTimeout)
	defer cancel()
	resp, err := a.forwardJSON(ctx, spec.Endpoint, http.MethodPost, spec.Path, body, http.Header{
		"Accept":       []string{"application/json"},
		"Content-Type": []string{"application/json"},
	})
	if err != nil {
		recordFailure(fmt.Errorf("provider recovery probe: %w", err))
		return
	}
	if parent.Err() != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		router.releaseHalfOpenProbe(candidate)
		return
	}
	if resp == nil {
		recordFailure(errors.New("provider recovery probe returned no response"))
		return
	}
	defer resp.Body.Close()
	_, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, providerRecoveryProbeMaxBodyBytes))
	if readErr != nil {
		recordFailure(fmt.Errorf("provider recovery probe response: %w", readErr))
		return
	}
	if parent.Err() != nil {
		router.releaseHalfOpenProbe(candidate)
		return
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		recordFailure(fmt.Errorf("provider recovery probe returned %s", resp.Status))
		return
	}
	router.recordSuccess(candidate)
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
