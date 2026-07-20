package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type config struct {
	Addr                              string               `json:"addr"`
	ActiveModelProfileID              string               `json:"active_model_profile_id,omitempty"`
	ModelProfiles                     []modelProfile       `json:"model_profiles,omitempty"`
	ActiveTextProfileID               string               `json:"active_text_profile_id"`
	TextModelProfiles                 []textModelProfile   `json:"text_model_profiles"`
	ActiveVisionProfileID             string               `json:"active_vision_profile_id"`
	VisionModelProfiles               []visionModelProfile `json:"vision_model_profiles"`
	TextProvider                      string               `json:"text_provider"`
	TextBaseURL                       string               `json:"text_base_url"`
	TextAPIKey                        string               `json:"text_api_key"`
	TextModelOverride                 string               `json:"text_model_override"`
	TextModelOverrides                []string             `json:"text_model_overrides,omitempty"`
	TextModelMappings                 []textModelMapping   `json:"text_model_mappings,omitempty"`
	TextWireAPI                       string               `json:"text_wire_api"`
	TextSupportsImages                bool                 `json:"text_supports_images,omitempty"`
	ProxyURL                          string               `json:"proxy_url"`
	VisionProvider                    string               `json:"vision_provider"`
	VisionBaseURL                     string               `json:"vision_base_url"`
	VisionAPIKey                      string               `json:"vision_api_key"`
	VisionModel                       string               `json:"vision_model"`
	VisionPrompt                      string               `json:"vision_prompt"`
	VisionEnabled                     *bool                `json:"vision_enabled"`
	PreserveCodexOfficialAuthOnSwitch *bool                `json:"preserve_codex_official_auth_on_switch"`
	UnifyCodexSessionHistory          bool                 `json:"unify_codex_session_history"`
	ClientRouteEnabled                map[string]bool      `json:"client_route_enabled"`
	LocalAPIEnabled                   *bool                `json:"local_api_enabled"`
	ClientConfigPaths                 map[string]string    `json:"client_config_paths"`
	ClientProgramPaths                map[string]string    `json:"client_program_paths"`
	ClientAutoRestart                 map[string]bool      `json:"client_auto_restart"`
	ClientAutoStart                   map[string]bool      `json:"client_auto_start"`
	ClientPathsDetected               bool                 `json:"client_paths_detected"`
	ClientPathDetectionVersion        int                  `json:"client_path_detection_version"`
	OpenWindow                        bool                 `json:"open_window"`
	OpenBrowser                       bool                 `json:"open_browser"`
}

type textModelProfile struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Provider       string             `json:"provider"`
	BaseURL        string             `json:"base_url"`
	APIKey         string             `json:"api_key"`
	ModelOverride  string             `json:"model_override"`
	ModelOverrides []string           `json:"model_overrides,omitempty"`
	ModelMappings  []textModelMapping `json:"model_mappings,omitempty"`
	WireAPI        string             `json:"wire_api"`
	SupportsImages bool               `json:"supports_images,omitempty"`
	ProxyURL       string             `json:"proxy_url"`
}

type visionModelProfile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

type modelProfile struct {
	ID                 string             `json:"id"`
	Name               string             `json:"name"`
	TextProvider       string             `json:"text_provider"`
	TextBaseURL        string             `json:"text_base_url"`
	TextAPIKey         string             `json:"text_api_key"`
	TextModelOverride  string             `json:"text_model_override"`
	TextModelOverrides []string           `json:"text_model_overrides,omitempty"`
	TextModelMappings  []textModelMapping `json:"text_model_mappings,omitempty"`
	TextWireAPI        string             `json:"text_wire_api"`
	TextSupportsImages bool               `json:"text_supports_images,omitempty"`
	ProxyURL           string             `json:"proxy_url"`
	VisionProvider     string             `json:"vision_provider"`
	VisionBaseURL      string             `json:"vision_base_url"`
	VisionAPIKey       string             `json:"vision_api_key"`
	VisionModel        string             `json:"vision_model"`
}

type textModelMapping struct {
	Name              string  `json:"name"`
	Model             string  `json:"model"`
	ContextWindow     flexInt `json:"context_window,omitempty"`
	SupportsImages    bool    `json:"supports_images,omitempty"`
	ReasoningEffort   string  `json:"reasoning_effort,omitempty"`
	SupportsReasoning *bool   `json:"supports_reasoning,omitempty"` // legacy boolean setting
}

type flexInt int

func (v *flexInt) UnmarshalJSON(data []byte) error {
	var number int
	if err := json.Unmarshal(data, &number); err == nil {
		*v = flexInt(number)
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	if text == "" {
		*v = 0
		return nil
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return err
	}
	*v = flexInt(parsed)
	return nil
}

type endpoint struct {
	Provider      string
	BaseURL       string
	APIKey        string
	ModelOverride string
	WireAPI       string
	ProxyURL      string
}

type app struct {
	mu                      sync.RWMutex
	cfg                     config
	configPath              string
	dbPath                  string
	db                      *sql.DB
	httpClient              *http.Client
	lastVision              visionDebugInfo
	visionCache             map[string]string
	clientProgramController clientProgramController
	logMu                   sync.Mutex
	logs                    []requestLog
	nextLogID               int64
}

type requestLog struct {
	ID               int64     `json:"id"`
	At               time.Time `json:"at"`
	Method           string    `json:"method"`
	Path             string    `json:"path"`
	Protocol         string    `json:"protocol"`
	Model            string    `json:"model"`
	UpstreamName     string    `json:"upstream_name"`
	UpstreamProvider string    `json:"upstream_provider"`
	ClientName       string    `json:"-"`
	ClientKeyPreview string    `json:"-"`
	Status           int       `json:"status"`
	DurationMS       int64     `json:"duration_ms"`
	FirstTokenMS     int64     `json:"first_token_ms"`
	RequestMode      string    `json:"request_mode"`
	InputTokens      int64     `json:"input_tokens"`
	OutputTokens     int64     `json:"output_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	CacheHitTokens   int64     `json:"cache_hit_tokens"`
	CacheWriteTokens int64     `json:"-"`
	RequestText      string    `json:"request_text"`
	ResponseText     string    `json:"response_text"`
	Error            string    `json:"error,omitempty"`
}

type visionDebugInfo struct {
	At         time.Time `json:"at"`
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	UserText   string    `json:"user_text"`
	ImageCount int       `json:"image_count"`
	Text       string    `json:"text"`
	Cached     bool      `json:"cached,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type imageRef struct {
	URL       string
	MediaType string
	Base64    string
}

type parsedMessage struct {
	Message message
	Text    string
	Images  []imageRef
}
