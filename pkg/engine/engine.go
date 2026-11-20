package engine

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"github.com/newprophecy2-cloud/Anizone-Prophecy-sub-server-/pkg/anizone"
	"github.com/newprophecy2-cloud/Anizone-Prophecy-sub-server-/pkg/config"
)

var Actions = []string{
	"search",
	"details",
	"episodes",
	"sources",
	"resolve",
	"subtitles",
}

var ErrorRetryability = map[string]bool{
	"NETWORK_ERROR":       true,
	"UPSTREAM_HTTP_ERROR": true,
	"TIMEOUT":             true,

	"NOT_FOUND":           false,
	"INVALID_ID":          false,
	"INVALID_PAGE":        false,
	"INVALID_PARAMETERS":  false,
	"PARSE_ERROR":         false,
	"INVALID_RESPONSE":    false,
	"INVALID_ENCODING":    false,
	"RESPONSE_TOO_LARGE":  false,
	"INVALID_BASE_URL":    false,
	"PAGINATION_ERROR":    false,
	"PAGINATION_LIMIT":    false,
	"PROVIDER_DISABLED":   false,
	"SOURCE_NOT_FOUND":    false,
	"SOURCE_UNAVAILABLE":  true,
	"UNSUPPORTED_ACTION":  false,
}

type ProviderEngine struct {
	client *anizone.Client
	cfg    *config.Config
}

func NewProviderEngine(cfg *config.Config) (*ProviderEngine, error) {
	client, err := anizone.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &ProviderEngine{
		client: client,
		cfg:    cfg,
	}, nil
}

func (e *ProviderEngine) Close() error {
	if e.client != nil {
		return e.client.Close()
	}
	return nil
}

func (e *ProviderEngine) enabled(providerID string) bool {
	if providerID == "anizone" {
		return e.cfg.AniZoneEnabled
	}
	return false
}

func (e *ProviderEngine) ProviderMetadata(providerID string) map[string]any {
	if providerID != "anizone" || e.client == nil {
		return map[string]any{}
	}

	meta := e.client.Metadata()
	result := make(map[string]any)
	for k, v := range meta {
		result[k] = v
	}

	result["id"] = providerID
	result["enabled"] = e.enabled(providerID)
	result["actions"] = Actions

	return result
}

func (e *ProviderEngine) ListProviders() []map[string]any {
	return []map[string]any{
		e.ProviderMetadata("anizone"),
	}
}

func (e *ProviderEngine) GetProvider(providerID string) map[string]any {
	if providerID != "anizone" {
		return nil
	}
	return e.ProviderMetadata(providerID)
}

func (e *ProviderEngine) ErrorResponse(code, message, provider, action string, retryable bool) map[string]any {
	return map[string]any{
		"status": "error",
		"error": map[string]any{
			"code":      code,
			"message":   message,
			"provider":  provider,
			"action":    action,
			"retryable": retryable,
		},
	}
}

func (e *ProviderEngine) Execute(ctx context.Context, providerID, action string, params map[string]any) map[string]any {
	if params == nil {
		params = make(map[string]any)
	}

	if providerID != "anizone" {
		return e.ErrorResponse("PROVIDER_NOT_FOUND", "Provider was not found.", providerID, action, false)
	}

	if !e.enabled(providerID) {
		return e.ErrorResponse("PROVIDER_DISABLED", "Provider is disabled.", providerID, action, false)
	}

	actionSupported := false
	for _, a := range Actions {
		if a == action {
			actionSupported = true
			break
		}
	}
	if !actionSupported {
		return e.ErrorResponse("UNKNOWN_ACTION", "Unsupported provider action.", providerID, action, false)
	}

	var result any
	var err error

	switch action {
	case "search":
		query, _ := params["query"].(string)
		page := 1
		if p, ok := params["page"].(float64); ok {
			page = int(p)
		} else if p, ok := params["page"].(int); ok {
			page = p
		} else if p, ok := params["page"].(string); ok {
			if pi, errConv := strconv.Atoi(p); errConv == nil {
				page = pi
			}
		}
		result, err = e.client.Search(ctx, query, page)

	case "details":
		slug, _ := params["slug"].(string)
		result, err = e.client.Details(ctx, slug)

	case "episodes":
		slug, _ := params["slug"].(string)
		result, err = e.client.Episodes(ctx, slug)

	case "sources":
		slug, _ := params["slug"].(string)
		episodeID := getStringParam(params, "episode_id")
		var langPtr, qualPtr *string
		if l, ok := params["language"].(string); ok && l != "" {
			langPtr = &l
		}
		if q, ok := params["quality"].(string); ok && q != "" {
			qualPtr = &q
		}
		result, err = e.client.Sources(ctx, slug, episodeID, langPtr, qualPtr)

	case "resolve":
		slug, _ := params["slug"].(string)
		episodeID := getStringParam(params, "episode_id")
		var srcIDPtr, langPtr, qualPtr *string
		if s := getStringParam(params, "source_id"); s != "" {
			srcIDPtr = &s
		}
		if l, ok := params["language"].(string); ok && l != "" {
			langPtr = &l
		}
		if q, ok := params["quality"].(string); ok && q != "" {
			qualPtr = &q
		}
		result, err = e.client.Resolve(ctx, slug, episodeID, srcIDPtr, langPtr, qualPtr)

	case "subtitles":
		slug, _ := params["slug"].(string)
		episodeID := getStringParam(params, "episode_id")
		result, err = e.client.Subtitles(ctx, slug, episodeID)

	default:
		return e.ErrorResponse("ACTION_NOT_IMPLEMENTED", fmt.Sprintf("Provider action '%s' is not implemented.", action), providerID, action, false)
	}

	if err != nil {
		code := "INTERNAL_ERROR"
		message := err.Error()

		if azErr, ok := err.(*anizone.AniZoneError); ok {
			code = azErr.Code
			message = azErr.Message
		}

		retryable := ErrorRetryability[code]

		if code == "INTERNAL_ERROR" {
			log.Printf("Unexpected provider error: %s / %s: %v", providerID, action, err)
			message = "An internal provider error occurred."
		}

		return e.ErrorResponse(code, message, providerID, action, retryable)
	}

	return map[string]any{
		"status":   "success",
		"provider": providerID,
		"action":   action,
		"data":     result,
	}
}

func getStringParam(params map[string]any, key string) string {
	if val, ok := params[key]; ok {
		switch v := val.(type) {
		case string:
			return v
		case float64:
			return strconv.Itoa(int(v))
		case int:
			return strconv.Itoa(v)
		}
	}
	return ""
}
