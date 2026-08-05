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

	"vision-relay/backend/internal/protocol"
)

const (
	providerFailureThreshold = 5
	providerCircuitCooldown  = 30 * time.Second
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
	once           sync.Once
	group          providerGroup
	candidates     []providerRouteCandidate
	configured     bool
	path           string
	originalModel  string
	containsImages bool
}

type providerRouteTrace struct {
	mu       sync.RWMutex
	selected providerRouteSelection
}

type providerRouteSelection struct {
	Group         providerGroup
	ProfileID     string
	Name          string
	Provider      string
	Model         string
	TransformKind string
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

func (a *app) resolveProviderRoutes(ctx context.Context, primary endpoint) ([]providerRouteCandidate, bool) {
	route := providerRouteRequestFromContext(ctx)
	if route == nil || !route.group.valid() {
		return nil, false
	}
	route.once.Do(func() {
		route.candidates, route.configured = providerRouteCandidatesForGroup(a.currentConfig(), route.group, primary)
	})
	return route.candidates, route.configured
}

func (a *app) resolveProviderRoute(ctx context.Context, primary endpoint) (providerRouteCandidate, bool) {
	candidates, configured := a.resolveProviderRoutes(ctx, primary)
	if !configured || len(candidates) == 0 {
		return providerRouteCandidate{}, false
	}
	return candidates[0], true
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
	FailureCount        int64
	ConsecutiveFailures int
	CircuitState        providerCircuitState
	OpenUntil           time.Time
	RuntimeGeneration   uint64
	HalfOpenInFlight    bool
	HalfOpenToken       uint64
	LastError           string
	LastFailureAt       time.Time
	LastSuccessAt       time.Time
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
		if classifyProviderResult(b.ctx, nil, readErr) == providerResultRetryableFailure {
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
	mu                    sync.Mutex
	groups                map[providerGroup]*providerGroupRuntime
	now                   func() time.Time
	nextRuntimeGeneration uint64
	nextHalfOpenToken     uint64
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
	Group             providerGroup
	ProfileID         string
	Name              string
	Config            config
	Endpoint          endpoint
	runtimeGeneration uint64
	halfOpenToken     uint64
}

func providerRouteCandidatesForGroup(cfg config, group providerGroup, primary endpoint) ([]providerRouteCandidate, bool) {
	profiles := normalizeTextProfiles(cfg.TextModelProfiles)
	byID := make(map[string]textModelProfile, len(profiles))
	for _, profile := range profiles {
		if profile.Client == string(group) {
			byID[profile.ID] = profile
		}
	}

	if providerFailoverEnabled(cfg) {
		queue := normalizeProviderFailoverProfiles(profiles, cfg.ProviderFailoverProfiles)[string(group)]
		if len(queue) > 0 {
			candidates := make([]providerRouteCandidate, 0, len(queue))
			for _, profileID := range queue {
				if profile, ok := byID[profileID]; ok {
					candidates = append(candidates, providerRouteCandidateForProfile(cfg, group, profile))
				}
			}
			if len(candidates) > 0 {
				return candidates, true
			}
		}
	}

	active := normalizeActiveTextProfilesByClient(profiles, cfg.ActiveTextProfileByClient, cfg.ActiveTextProfileID)[string(group)]
	if profile, ok := byID[active]; ok {
		return []providerRouteCandidate{providerRouteCandidateForProfile(cfg, group, profile)}, true
	}
	if cfg.legacyTextRouting || len(cfg.TextModelProfiles) == 0 {
		// Preserve only genuine configurations that predate client groups. The
		// runtime state remains namespaced by group, so failures never cross them.
		return []providerRouteCandidate{{
			Group: group, ProfileID: "legacy-" + string(group), Name: "\u5f53\u524d\u6587\u672c\u4e0a\u6e38",
			Config: cfg, Endpoint: primary,
		}}, true
	}
	return nil, false
}

func providerRouteCandidateForGroup(cfg config, group providerGroup, primary endpoint) (providerRouteCandidate, bool) {
	candidates, configured := providerRouteCandidatesForGroup(cfg, group, primary)
	if !configured || len(candidates) == 0 {
		return providerRouteCandidate{Group: group}, false
	}
	return candidates[0], true
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
	candidate.runtimeGeneration = 0
	candidate.halfOpenToken = 0
	now := r.now()
	state := r.providerStateLocked(candidate.Group, candidate.ProfileID)
	candidate.runtimeGeneration = state.RuntimeGeneration
	if state.CircuitState == providerCircuitOpen {
		if state.OpenUntil.IsZero() || now.Before(state.OpenUntil) || state.HalfOpenInFlight {
			return candidate, false
		}
		state.CircuitState = providerCircuitHalfOpen
	}
	if state.CircuitState == providerCircuitHalfOpen {
		if state.HalfOpenInFlight {
			return candidate, false
		}
		r.nextHalfOpenToken++
		if r.nextHalfOpenToken == 0 {
			r.nextHalfOpenToken++
		}
		state.HalfOpenInFlight = true
		state.HalfOpenToken = r.nextHalfOpenToken
		candidate.halfOpenToken = state.HalfOpenToken
	}
	return candidate, true
}

// providerConfiguredCandidates includes every explicit supplier plus the
// synthetic per-client candidates used by legacy global routing. It is used
// only to reconcile runtime circuit state when provider configuration changes;
// it never sends a request to a provider.
func providerConfiguredCandidates(cfg config) []providerRouteCandidate {
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

func providerRuntimeProfile(candidate providerRouteCandidate) textModelProfile {
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
	for _, candidate := range providerConfiguredCandidates(cfg) {
		profiles[providerProfileRuntimeKey{Group: candidate.Group, ProfileID: candidate.ProfileID}] = providerRuntimeProfile(candidate)
	}
	return profiles
}

func sameProviderRuntimeConfig(left, right textModelProfile) bool {
	// Display-name changes do not affect provider routing.
	left.Name = ""
	right.Name = ""
	return reflect.DeepEqual(left, right)
}

// reconcileConfigChange clears stale circuit state when an endpoint, API key,
// proxy, protocol, or model configuration changes.
func (r *providerRouter) reconcileConfigChange(previous, current config) {
	previousProfiles := providerRuntimeProfiles(previous)
	currentProfiles := providerRuntimeProfiles(current)

	r.mu.Lock()
	defer r.mu.Unlock()
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
		if state != nil {
			*state = providerRuntimeState{
				CircuitState:      providerCircuitClosed,
				RuntimeGeneration: r.nextRuntimeGenerationLocked(),
			}
		}
	}
}

func (r *providerRouter) providerStateLocked(group providerGroup, profileID string) *providerRuntimeState {
	groupState := r.groups[group]
	if groupState == nil {
		groupState = &providerGroupRuntime{Providers: map[string]*providerRuntimeState{}}
		r.groups[group] = groupState
	}
	state := groupState.Providers[profileID]
	if state == nil {
		state = &providerRuntimeState{
			CircuitState:      providerCircuitClosed,
			RuntimeGeneration: r.nextRuntimeGenerationLocked(),
		}
		groupState.Providers[profileID] = state
	}
	return state
}

func (r *providerRouter) nextRuntimeGenerationLocked() uint64 {
	r.nextRuntimeGeneration++
	if r.nextRuntimeGeneration == 0 {
		r.nextRuntimeGeneration++
	}
	return r.nextRuntimeGeneration
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
	if candidate.runtimeGeneration == 0 || candidate.runtimeGeneration != state.RuntimeGeneration {
		return
	}
	if candidate.halfOpenToken != 0 {
		if !matchingHalfOpenProbe(state, candidate) {
			return
		}
	} else if state.CircuitState != providerCircuitClosed {
		// A request selected while the circuit was closed may finish after a
		// different request has opened it. Its stale success must not bypass
		// the cooldown and half-open transition.
		return
	}
	state.ConsecutiveFailures = 0
	state.CircuitState = providerCircuitClosed
	state.OpenUntil = time.Time{}
	state.HalfOpenInFlight = false
	state.HalfOpenToken = 0
	state.LastError = ""
	state.LastSuccessAt = r.now()
}

func (r *providerRouter) releaseHalfOpenProbe(candidate providerRouteCandidate) {
	if candidate.halfOpenToken == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.providerStateLocked(candidate.Group, candidate.ProfileID)
	if !matchingHalfOpenProbe(state, candidate) {
		return
	}
	state.HalfOpenInFlight = false
	state.HalfOpenToken = 0
}

func (r *providerRouter) recordFailure(candidate providerRouteCandidate, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.providerStateLocked(candidate.Group, candidate.ProfileID)
	if candidate.runtimeGeneration == 0 || candidate.runtimeGeneration != state.RuntimeGeneration {
		return
	}
	if candidate.halfOpenToken != 0 {
		if !matchingHalfOpenProbe(state, candidate) {
			return
		}
	} else if state.CircuitState != providerCircuitClosed {
		// Do not let a stale request refresh an existing open cooldown or
		// interfere with the currently active half-open probe.
		return
	}
	state.FailureCount++
	state.ConsecutiveFailures++
	state.HalfOpenInFlight = false
	state.HalfOpenToken = 0
	state.LastFailureAt = r.now()
	if err != nil {
		state.LastError = err.Error()
	}
	if state.ConsecutiveFailures >= providerFailureThreshold || candidate.halfOpenToken != 0 {
		state.CircuitState = providerCircuitOpen
		state.OpenUntil = state.LastFailureAt.Add(providerCircuitCooldown)
	} else {
		state.CircuitState = providerCircuitClosed
	}
}

func matchingHalfOpenProbe(state *providerRuntimeState, candidate providerRouteCandidate) bool {
	return candidate.runtimeGeneration != 0 &&
		state.RuntimeGeneration == candidate.runtimeGeneration &&
		state.CircuitState == providerCircuitHalfOpen &&
		state.HalfOpenInFlight &&
		state.HalfOpenToken != 0 &&
		state.HalfOpenToken == candidate.halfOpenToken
}

type providerResultCategory uint8

const (
	providerResultSuccess providerResultCategory = iota
	providerResultRetryableFailure
	providerResultNeutral
)

func classifyProviderResult(ctx context.Context, resp *http.Response, err error) providerResultCategory {
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return providerResultNeutral
	}
	if errors.Is(err, context.Canceled) {
		return providerResultNeutral
	}
	if err != nil {
		return providerResultRetryableFailure
	}
	if resp == nil {
		return providerResultRetryableFailure
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return providerResultSuccess
	}
	switch resp.StatusCode {
	case http.StatusBadRequest,
		http.StatusMethodNotAllowed,
		http.StatusNotAcceptable,
		http.StatusRequestEntityTooLarge,
		http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType,
		http.StatusUnprocessableEntity,
		http.StatusNotImplemented:
		return providerResultNeutral
	default:
		return providerResultRetryableFailure
	}
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

func providerRouteCandidatesCompatible(route *providerRouteRequest, prepared, candidate providerRouteCandidate) bool {
	if !providerRouteCanAdaptPerCandidate(route) && providerRequestTransformKind(prepared.Group, prepared.Config) != providerRequestTransformKind(candidate.Group, candidate.Config) {
		return false
	}
	if route == nil || !route.containsImages {
		return true
	}
	return textSupportsImages(prepared.Config, route.originalModel) == textSupportsImages(candidate.Config, route.originalModel)
}

func providerRouteCanAdaptPerCandidate(route *providerRouteRequest) bool {
	return route != nil && route.group == providerGroupCodex && isOpenAIResponsesPath(route.path)
}

func providerRouteSelectedTransformKind(ctx context.Context) (string, bool) {
	if trace := providerRouteTraceFromContext(ctx); trace != nil {
		if selection, ok := trace.get(); ok && selection.TransformKind != "" {
			return selection.TransformKind, true
		}
	}
	return "", false
}

func providerRequestTransformKind(group providerGroup, cfg config) string {
	switch group {
	case providerGroupCodex:
		if normalizeProvider(cfg.TextProvider) == "openai" && normalizeWireAPI(cfg.TextWireAPI) != "responses" {
			return "openai_chat"
		}
		return "native_responses"
	case providerGroupClaude:
		if normalizeProvider(cfg.TextProvider) == "openai" {
			return "openai_chat"
		}
		return "native_anthropic"
	default:
		return "native"
	}
}

func adaptProviderAttempt(candidate providerRouteCandidate, requestURI string, body []byte, originalRequested string) (string, []byte) {
	modelRequestURI := requestURI
	if originalRequested != "" && geminiRequestedModel(requestURI) != "" {
		modelRequestURI = replaceGeminiRequestedModel(requestURI, originalRequested)
	}
	adaptedURI := geminiRequestURIWithEffectiveModel(candidate.Config, modelRequestURI)
	if len(body) == 0 {
		return adaptedURI, body
	}
	payload := decodeJSONMap(body)
	if payload == nil {
		return adaptedURI, body
	}
	changed := false
	if providerAttemptNeedsResponsesToChat(candidate, adaptedURI) {
		chatPayload := protocol.ResponsesPayloadToChatCompletions(payload)
		ensureStreamUsage(chatPayload)
		sanitizeOpenAIChatPayload(chatPayload)
		payload = chatPayload
		adaptedURI = replaceRequestURIPath(adaptedURI, "/v1/chat/completions")
		changed = true
	}
	current := strings.TrimSpace(firstString(payload["model"]))
	if current == "" {
		if changed {
			if encoded, err := jsonMarshal(payload); err == nil {
				return adaptedURI, encoded
			}
		}
		return adaptedURI, body
	}
	requested := strings.TrimSpace(originalRequested)
	if requested == "" {
		requested = current
	}
	desired := requested
	if model := effectiveTextModel(candidate.Config, requested); model != "" {
		desired = model
	}
	if desired != "" && desired != current {
		payload["model"] = desired
		changed = true
	}
	if changed {
		if encoded, err := jsonMarshal(payload); err == nil {
			return adaptedURI, encoded
		}
	}
	return adaptedURI, body
}

func providerAttemptNeedsResponsesToChat(candidate providerRouteCandidate, requestURI string) bool {
	if candidate.Group != providerGroupCodex || providerRequestTransformKind(candidate.Group, candidate.Config) != "openai_chat" {
		return false
	}
	path, _ := splitRequestURIPathQuery(requestURI)
	return isOpenAIResponsesPath(path)
}

func splitRequestURIPathQuery(requestURI string) (string, string) {
	if idx := strings.Index(requestURI, "?"); idx >= 0 {
		return requestURI[:idx], requestURI[idx:]
	}
	return requestURI, ""
}

func replaceRequestURIPath(requestURI, path string) string {
	_, query := splitRequestURIPathQuery(requestURI)
	return path + query
}

func replaceGeminiRequestedModel(requestURI, model string) string {
	for _, prefix := range []string{"/v1beta/models/", "/v1/models/"} {
		if !strings.HasPrefix(requestURI, prefix) {
			continue
		}
		modelStart := len(prefix)
		suffixIndex := strings.Index(requestURI[modelStart:], ":")
		if suffixIndex < 0 {
			return requestURI
		}
		modelEnd := modelStart + suffixIndex
		return requestURI[:modelStart] + model + requestURI[modelEnd:]
	}
	return requestURI
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
