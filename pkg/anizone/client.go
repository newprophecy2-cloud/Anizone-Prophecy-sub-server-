package anizone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/newprophecy2-cloud/Anizone-Prophecy-sub-server-/pkg/config"
)

type AniZoneError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
	Err     error  `json:"-"`
}

func (e *AniZoneError) Error() string {
	return e.Message
}

func (e *AniZoneError) Unwrap() error {
	return e.Err
}

func NewAniZoneError(message, code string, err ...error) *AniZoneError {
	var cause error
	if len(err) > 0 {
		cause = err[0]
	}
	return &AniZoneError{
		Message: message,
		Code:    code,
		Err:     cause,
	}
}

type SearchItem struct {
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	MediaType    string `json:"media_type"`
	Year         *int   `json:"year"`
	Poster       *string `json:"poster"`
	EpisodeCount *int   `json:"episode_count"`
}

type EpisodeItem struct {
	ID          string  `json:"id"`
	Number      int     `json:"number"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Season      *int    `json:"season"`
}

type EpisodePage struct {
	Items       []map[string]any  `json:"items"`
	Snapshot    string            `json:"snapshot"`
	Cursor      *string           `json:"cursor"`
	HasMore     bool              `json:"has_more"`
	CSRF        string            `json:"csrf"`
	Cookies     map[string]string `json:"cookies"`
	SeriesTitle *string           `json:"series_title"`
}

type Client struct {
	baseURL            string
	timeout            time.Duration
	maxRetries         int
	maxResponseBytes   int64
	maxPages           int
	userAgent          string
	customCookie       string
	httpClient         *http.Client
}

func NewClient(cfg *config.Config) (*Client, error) {
	baseURL := strings.TrimRight(cfg.AniZoneBaseURL, "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, NewAniZoneError("AniZone base URL must use HTTPS", "INVALID_BASE_URL")
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}

	if cfg.ProxyURL != "" {
		if proxyURI, err := url.Parse(cfg.ProxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxyURI)
		}
	}

	client := &http.Client{
		Timeout:   cfg.RequestTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		},
	}

	return &Client{
		baseURL:          baseURL,
		timeout:          cfg.RequestTimeout,
		maxRetries:       cfg.RequestRetries,
		maxResponseBytes: cfg.MaxResponseBytes,
		maxPages:         cfg.MaxPaginationPages,
		userAgent:        cfg.UserAgent,
		customCookie:     cfg.CustomCookie,
		httpClient:       client,
	}, nil
}

func (c *Client) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

func (c *Client) buildURL(path string) string {
	return strings.TrimRight(c.baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func (c *Client) request(ctx context.Context, method, path string, params map[string]string, jsonBody any, extraHeaders map[string]string) ([]byte, http.Header, int, error) {
	reqURL := c.buildURL(path)
	if len(params) > 0 {
		parsedURL, err := url.Parse(reqURL)
		if err == nil {
			q := parsedURL.Query()
			for k, v := range params {
				q.Set(k, v)
			}
			parsedURL.RawQuery = q.Encode()
			reqURL = parsedURL.String()
		}
	}

	var lastError error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		var bodyReader io.Reader
		if jsonBody != nil {
			jsonBytes, err := json.Marshal(jsonBody)
			if err != nil {
				return nil, nil, 0, NewAniZoneError("Failed to encode JSON request body", "INVALID_PARAMETERS", err)
			}
			bodyReader = bytes.NewReader(jsonBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
		if err != nil {
			return nil, nil, 0, NewAniZoneError(fmt.Sprintf("Failed to create request: %v", err), "NETWORK_ERROR", err)
		}

		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

		if c.customCookie != "" {
			req.Header.Set("Cookie", c.customCookie)
		}

		for k, v := range extraHeaders {
			if k == "Cookie" && c.customCookie != "" {
				req.Header.Set("Cookie", c.customCookie+"; "+v)
			} else {
				req.Header.Set(k, v)
			}
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastError = err
			if attempt < c.maxRetries {
				sleepSec := math.Min(0.5*float64(attempt+1), 3.0)
				time.Sleep(time.Duration(sleepSec * float64(time.Second)))
				continue
			}
			return nil, nil, 0, NewAniZoneError(fmt.Sprintf("AniZone request failed: %v", lastError), "NETWORK_ERROR", lastError)
		}

		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return nil, resp.Header, resp.StatusCode, NewAniZoneError("AniZone item not found", "NOT_FOUND")
		}

		if resp.StatusCode >= 400 {
			switch resp.StatusCode {
			case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
				http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
				lastError = fmt.Errorf("upstream returned status %d", resp.StatusCode)
				if attempt < c.maxRetries {
					sleepSec := math.Min(0.5*float64(attempt+1), 3.0)
					time.Sleep(time.Duration(sleepSec * float64(time.Second)))
					continue
				}
				return nil, resp.Header, resp.StatusCode, NewAniZoneError(fmt.Sprintf("AniZone HTTP error: %d", resp.StatusCode), "UPSTREAM_HTTP_ERROR")
			default:
				return nil, resp.Header, resp.StatusCode, NewAniZoneError(fmt.Sprintf("AniZone HTTP error: %d", resp.StatusCode), "UPSTREAM_HTTP_ERROR")
			}
		}

		lr := io.LimitReader(resp.Body, c.maxResponseBytes+1)
		bodyBytes, err := io.ReadAll(lr)
		if err != nil {
			lastError = err
			if attempt < c.maxRetries {
				continue
			}
			return nil, resp.Header, resp.StatusCode, NewAniZoneError(fmt.Sprintf("Failed to read response: %v", err), "NETWORK_ERROR", err)
		}

		if int64(len(bodyBytes)) > c.maxResponseBytes {
			return nil, resp.Header, resp.StatusCode, NewAniZoneError("Response exceeded configured size limit", "RESPONSE_TOO_LARGE")
		}

		return bodyBytes, resp.Header, resp.StatusCode, nil
	}

	return nil, nil, 0, NewAniZoneError(fmt.Sprintf("AniZone request failed: %v", lastError), "NETWORK_ERROR", lastError)
}

func (c *Client) getPage(ctx context.Context, path string, params map[string]string) (string, []string, error) {
	headers := map[string]string{
		"Referer": c.baseURL + "/",
	}
	bytes, respHeader, _, err := c.request(ctx, http.MethodGet, path, params, nil, headers)
	if err != nil {
		return "", nil, err
	}
	cookies := respHeader["Set-Cookie"]
	return string(bytes), cookies, nil
}

// --------------------------------------------------
// SEARCH
// --------------------------------------------------

func (c *Client) Search(ctx context.Context, query string, page int) ([]map[string]any, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []map[string]any{}, nil
	}

	if page < 1 || page > c.maxPages {
		return nil, NewAniZoneError("Invalid search page", "INVALID_PAGE")
	}

	content, cookies, err := c.getPage(ctx, "/anime", map[string]string{"search": query})
	if err != nil {
		return nil, err
	}

	state, err := c.initialPage(content, cookies)
	if err != nil {
		return nil, err
	}

	seenCursors := make(map[string]bool)

	for i := 1; i < page; i++ {
		if !state.HasMore {
			state.Items = []map[string]any{}
			break
		}

		if state.Cursor == nil || *state.Cursor == "" {
			return nil, NewAniZoneError("Missing search pagination cursor", "PAGINATION_ERROR")
		}

		if seenCursors[*state.Cursor] {
			return nil, NewAniZoneError("Repeated search pagination cursor", "PAGINATION_ERROR")
		}

		seenCursors[*state.Cursor] = true

		nextState, err := c.loadPage(ctx, "/anime", state)
		if err != nil {
			return nil, err
		}
		state = nextState
	}

	parsed := c.parseSearchItems(state.Items)
	results := make([]map[string]any, 0, len(parsed))
	for _, item := range parsed {
		results = append(results, map[string]any{
			"id":            item.Slug,
			"provider":      "anizone",
			"title":         item.Title,
			"media_type":    item.MediaType,
			"year":          item.Year,
			"poster":        item.Poster,
			"episode_count": item.EpisodeCount,
			"season_count":  nil,
		})
	}

	return results, nil
}

// --------------------------------------------------
// DETAILS
// --------------------------------------------------

func (c *Client) Details(ctx context.Context, slug string) (map[string]any, error) {
	if err := c.validateSlug(slug); err != nil {
		return nil, err
	}

	state, err := c.scrapeSeries(ctx, slug)
	if err != nil {
		return nil, err
	}

	episodes := c.parseEpisodes(state.Items)
	seasonsMap := make(map[int][]map[string]any)

	for _, ep := range episodes {
		seasonNumber := 1
		if ep.Season != nil && *ep.Season > 0 {
			seasonNumber = *ep.Season
		}

		seasonsMap[seasonNumber] = append(seasonsMap[seasonNumber], map[string]any{
			"id":                  ep.ID,
			"number":              ep.Number,
			"season":              seasonNumber,
			"title":               ep.Title,
			"description":         ep.Description,
			"provider_episode_id": ep.ID,
			"provider_season_id":  strconv.Itoa(seasonNumber),
		})
	}

	seasonNums := make([]int, 0, len(seasonsMap))
	for sNum := range seasonsMap {
		seasonNums = append(seasonNums, sNum)
	}
	sort.Ints(seasonNums)

	seasonData := make([]map[string]any, 0, len(seasonNums))
	for _, sNum := range seasonNums {
		eps := seasonsMap[sNum]
		sort.Slice(eps, func(i, j int) bool {
			return eps[i]["number"].(int) < eps[j]["number"].(int)
		})
		seasonData = append(seasonData, map[string]any{
			"number":             sNum,
			"provider_season_id": strconv.Itoa(sNum),
			"episodes":           eps,
		})
	}

	title := strings.ReplaceAll(slug, "-", " ")
	if state.SeriesTitle != nil && *state.SeriesTitle != "" {
		title = *state.SeriesTitle
	}

	return map[string]any{
		"id":           slug,
		"provider":     "anizone",
		"title":        title,
		"media_type":   "series",
		"year":         nil,
		"description":  nil,
		"poster":       nil,
		"genres":       []string{},
		"season_count": len(seasonData),
		"seasons":      seasonData,
	}, nil
}

// --------------------------------------------------
// EPISODES
// --------------------------------------------------

func (c *Client) Episodes(ctx context.Context, slug string) (map[string]any, error) {
	if err := c.validateSlug(slug); err != nil {
		return nil, err
	}

	state, err := c.scrapeSeries(ctx, slug)
	if err != nil {
		return nil, err
	}

	parsed := c.parseEpisodes(state.Items)
	episodes := make([]map[string]any, 0, len(parsed))
	for _, ep := range parsed {
		seasonNum := 1
		if ep.Season != nil && *ep.Season > 0 {
			seasonNum = *ep.Season
		}
		episodes = append(episodes, map[string]any{
			"id":          ep.ID,
			"number":      ep.Number,
			"season":      seasonNum,
			"title":       ep.Title,
			"description": ep.Description,
		})
	}

	return map[string]any{
		"provider": "anizone",
		"anime_id": slug,
		"title":    state.SeriesTitle,
		"episodes": episodes,
	}, nil
}

func (c *Client) scrapeSeries(ctx context.Context, slug string) (*EpisodePage, error) {
	content, cookies, err := c.getPage(ctx, "/anime/"+slug, nil)
	if err != nil {
		return nil, err
	}

	state, err := c.initialPage(content, cookies)
	if err != nil {
		return nil, err
	}

	seenCursors := make(map[string]bool)
	pageCount := 1

	for state.HasMore && state.Cursor != nil && *state.Cursor != "" {
		cursor := *state.Cursor
		if seenCursors[cursor] {
			return nil, NewAniZoneError("Repeated episode pagination cursor", "PAGINATION_ERROR")
		}
		seenCursors[cursor] = true

		nextState, err := c.loadPage(ctx, "/anime/"+slug, state)
		if err != nil {
			return nil, err
		}

		nextState.Items = c.mergeItems(state.Items, nextState.Items)
		state = nextState
		pageCount++

		if pageCount >= c.maxPages && state.HasMore {
			return nil, NewAniZoneError("Episode pagination limit exceeded", "PAGINATION_LIMIT")
		}
	}

	if len(state.Items) == 0 {
		return nil, NewAniZoneError("No episodes found", "NOT_FOUND")
	}

	return state, nil
}

// --------------------------------------------------
// LIVEWIRE
// --------------------------------------------------

func (c *Client) loadPage(ctx context.Context, refererPath string, state *EpisodePage) (*EpisodePage, error) {
	if state.Cursor == nil || *state.Cursor == "" {
		return nil, NewAniZoneError("Missing pagination cursor", "PAGINATION_ERROR")
	}

	body := map[string]any{
		"components": []map[string]any{
			{
				"snapshot": state.Snapshot,
				"updates":  map[string]any{},
				"calls": []map[string]any{
					{
						"path":   "",
						"method": "loadPage",
						"params": []any{*state.Cursor},
					},
				},
			},
		},
	}

	headers := map[string]string{
		"User-Agent":       c.userAgent,
		"Accept":           "application/json, text/plain, */*",
		"Content-Type":     "application/json",
		"X-Livewire":       "",
		"X-CSRF-TOKEN":     state.CSRF,
		"X-Requested-With": "XMLHttpRequest",
		"Origin":           c.baseURL,
		"Referer":          c.buildURL(refererPath),
		"Cookie":           c.cookieHeader(state.Cookies),
	}

	respBytes, respHeader, _, err := c.request(ctx, http.MethodPost, "/livewire/update", nil, body, headers)
	if err != nil {
		return nil, err
	}

	var parsedResp map[string]any
	if err := json.Unmarshal(respBytes, &parsedResp); err != nil {
		return nil, NewAniZoneError("Invalid Livewire JSON", "INVALID_RESPONSE", err)
	}

	components, ok := parsedResp["components"].([]any)
	if !ok || len(components) == 0 {
		return nil, NewAniZoneError("Missing Livewire component", "PAGINATION_ERROR")
	}

	component, ok := components[0].(map[string]any)
	if !ok {
		return nil, NewAniZoneError("Invalid Livewire component", "PAGINATION_ERROR")
	}

	snapshot, ok := component["snapshot"].(string)
	if !ok || snapshot == "" {
		return nil, NewAniZoneError("Missing Livewire snapshot", "PAGINATION_ERROR")
	}

	effects, _ := component["effects"].(map[string]any)
	dispatches, _ := effects["dispatches"].([]any)
	if dispatches == nil {
		return nil, NewAniZoneError("Missing Livewire dispatch", "PAGINATION_ERROR")
	}

	var itemsLoadedDispatch map[string]any
	for _, d := range dispatches {
		if dm, ok := d.(map[string]any); ok && dm["name"] == "items-loaded" {
			itemsLoadedDispatch = dm
			break
		}
	}

	if itemsLoadedDispatch == nil {
		return nil, NewAniZoneError("Missing items-loaded event", "PAGINATION_ERROR")
	}

	params, _ := itemsLoadedDispatch["params"].(map[string]any)
	rawItems, _ := params["items"].([]any)
	if rawItems == nil {
		return nil, NewAniZoneError("Invalid pagination items", "PAGINATION_ERROR")
	}

	items := make([]map[string]any, 0, len(rawItems))
	for _, it := range rawItems {
		if itm, ok := it.(map[string]any); ok {
			items = append(items, itm)
		}
	}

	var nextCursor *string
	if nc, ok := params["nextCursor"].(string); ok && nc != "" {
		nextCursor = &nc
	}

	hasMore := false
	if hm, ok := params["hasMore"].(bool); ok {
		hasMore = hm
	}

	mergedCookies := c.mergeCookies(state.Cookies, respHeader["Set-Cookie"])

	return &EpisodePage{
		Items:       items,
		Snapshot:    snapshot,
		Cursor:      nextCursor,
		HasMore:     hasMore,
		CSRF:        state.CSRF,
		Cookies:     mergedCookies,
		SeriesTitle: state.SeriesTitle,
	}, nil
}

func (c *Client) initialPage(content string, cookies []string) (*EpisodePage, error) {
	itemsVal := c.jsonArgument(content, "items")
	rawSlice, ok := itemsVal.([]any)
	if !ok {
		return nil, NewAniZoneError("Initial items payload not found", "PARSE_ERROR")
	}

	items := make([]map[string]any, 0, len(rawSlice))
	for _, item := range rawSlice {
		if im, ok := item.(map[string]any); ok {
			items = append(items, im)
		}
	}

	marker := "items: JSON.parse('"
	markerIdx := strings.Index(content, marker)
	if markerIdx < 0 {
		return nil, NewAniZoneError("Livewire items marker not found", "PARSE_ERROR")
	}

	snapshot := c.htmlAttributeBefore(content, markerIdx)
	csrf := c.csrfToken(content)

	if snapshot == "" || csrf == "" {
		return nil, NewAniZoneError("Livewire state is incomplete", "PARSE_ERROR")
	}

	titlesVal := c.jsonArgument(content, "anmTitles")
	var seriesTitle *string
	if titlesMap, ok := titlesVal.(map[string]any); ok {
		seriesTitle = c.pickTitle(titlesMap)
	}

	nextCursor := c.jsPropertyString(content, "nextCursor")
	hasMore := c.jsPropertyBool(content, "hasMore")

	return &EpisodePage{
		Items:       items,
		Snapshot:    snapshot,
		Cursor:      nextCursor,
		HasMore:     hasMore,
		CSRF:        csrf,
		Cookies:     c.mergeCookies(nil, cookies),
		SeriesTitle: seriesTitle,
	}, nil
}

// --------------------------------------------------
// PARSERS
// --------------------------------------------------

var slugRegex = regexp.MustCompile(`^[A-Za-z0-9-]{1,180}$`)
var digitRegex = regexp.MustCompile(`^[0-9]{1,8}$`)

func (c *Client) parseSearchItems(items []map[string]any) []SearchItem {
	results := make([]SearchItem, 0, len(items))

	for _, item := range items {
		slug, _ := item["slug"].(string)
		if !slugRegex.MatchString(slug) {
			continue
		}

		var title string
		if titleList, ok := item["title_list"].(map[string]any); ok {
			if t := c.pickTitle(titleList); t != nil {
				title = *t
			}
		}
		if title == "" {
			if mt, ok := item["main_title"].(string); ok {
				title = mt
			}
		}

		title = strings.TrimSpace(title)
		if title == "" {
			continue
		}

		rawType, _ := item["type"].(string)
		mediaType := "series"
		if strings.ToLower(rawType) == "movie" {
			mediaType = "movie"
		}

		var year *int
		if y, ok := item["start_year"].(float64); ok {
			yi := int(y)
			year = &yi
		} else if y, ok := item["start_year"].(int); ok {
			year = &y
		}

		var poster *string
		if cov, ok := item["cover"].(string); ok && cov != "" {
			poster = &cov
		}

		var episodeCount *int
		if ec, ok := item["episode_count"].(float64); ok {
			eci := int(ec)
			episodeCount = &eci
		} else if ec, ok := item["episode_count"].(int); ok {
			episodeCount = &ec
		}

		results = append(results, SearchItem{
			Slug:         slug,
			Title:        title,
			MediaType:    mediaType,
			Year:         year,
			Poster:       poster,
			EpisodeCount: episodeCount,
		})
	}

	return results
}

func (c *Client) parseEpisodes(items []map[string]any) []EpisodeItem {
	results := make([]EpisodeItem, 0, len(items))
	seen := make(map[string]bool)

	for _, item := range items {
		var sourceNumber *int
		if rawSlug, ok := item["slug"].(string); ok && isAllDigits(rawSlug) {
			if n, err := strconv.Atoi(rawSlug); err == nil {
				sourceNumber = &n
			}
		}

		if sourceNumber == nil {
			if rawURL, ok := item["url"].(string); ok {
				trimmed := strings.TrimRight(rawURL, "/")
				lastSlash := strings.LastIndex(trimmed, "/")
				if lastSlash >= 0 {
					last := trimmed[lastSlash+1:]
					if isAllDigits(last) {
						if n, err := strconv.Atoi(last); err == nil {
							sourceNumber = &n
						}
					}
				}
			}
		}

		if sourceNumber == nil || *sourceNumber == 0 {
			continue
		}

		season := c.parseSeason(item)
		seasonVal := 1
		if season != nil {
			seasonVal = *season
		}
		identity := fmt.Sprintf("%d-%d", seasonVal, *sourceNumber)
		if seen[identity] {
			continue
		}
		seen[identity] = true

		var title string
		if titleList, ok := item["title_list"].(map[string]any); ok {
			if t := c.pickTitle(titleList); t != nil {
				title = *t
			}
		}
		if title == "" {
			title = fmt.Sprintf("Episode %d", *sourceNumber)
		}

		var summary *string
		if sum, ok := item["summary"].(string); ok && sum != "" {
			summary = &sum
		}

		results = append(results, EpisodeItem{
			ID:          strconv.Itoa(*sourceNumber),
			Number:      *sourceNumber,
			Title:       &title,
			Description: summary,
			Season:      season,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		s1 := 1
		if results[i].Season != nil {
			s1 = *results[i].Season
		}
		s2 := 1
		if results[j].Season != nil {
			s2 = *results[j].Season
		}
		if s1 != s2 {
			return s1 < s2
		}
		return results[i].Number < results[j].Number
	})

	return results
}

func (c *Client) parseSeason(item map[string]any) *int {
	keys := []string{"season", "season_number", "seasonNumber"}
	for _, k := range keys {
		val, exists := item[k]
		if !exists {
			continue
		}
		switch v := val.(type) {
		case int:
			if v > 0 {
				return &v
			}
		case float64:
			if v > 0 {
				vi := int(v)
				return &vi
			}
		case string:
			if vi, err := strconv.Atoi(v); err == nil && vi > 0 {
				return &vi
			}
		}
	}
	return nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func (c *Client) getEpisodeSourceNumber(ctx context.Context, slug string, episodeID string) (int, error) {
	if err := c.validateSlug(slug); err != nil {
		return 0, err
	}

	episodeID = strings.TrimSpace(episodeID)
	if !digitRegex.MatchString(episodeID) {
		return 0, NewAniZoneError("Invalid episode ID", "INVALID_EPISODE")
	}

	state, err := c.scrapeSeries(ctx, slug)
	if err != nil {
		return 0, err
	}

	episodes := c.parseEpisodes(state.Items)
	for _, ep := range episodes {
		if ep.ID == episodeID {
			return ep.Number, nil
		}
	}

	return 0, NewAniZoneError("Episode not found", "NOT_FOUND")
}

type WatchResult struct {
	URL          string           `json:"url"`
	Subtitles    []map[string]any `json:"subtitles"`
	SourceNumber int              `json:"source_number"`
	Referer      string           `json:"referer"`
}

func (c *Client) scrapeWatch(ctx context.Context, slug string, sourceNumber int) (*WatchResult, error) {
	if err := c.validateSlug(slug); err != nil {
		return nil, err
	}

	if sourceNumber < 1 {
		return nil, NewAniZoneError("Invalid episode number", "INVALID_EPISODE")
	}

	watchPath := fmt.Sprintf("/anime/%s/%d", slug, sourceNumber)
	content, _, err := c.getPage(ctx, watchPath, nil)
	if err != nil {
		return nil, err
	}

	playerVal := c.jsonCallArgument(content, "vidstackPlayer")
	player, ok := playerVal.(map[string]any)
	if !ok {
		return nil, NewAniZoneError("AniZone player payload not found", "PLAYER_NOT_FOUND")
	}

	streamURLRaw, _ := player["src"].(string)
	streamURL, err := c.normalizeMediaURL(streamURLRaw)
	if err != nil {
		return nil, err
	}

	subtitles := make([]map[string]any, 0)
	if rawSubtitles, ok := player["subtitles"].([]any); ok {
		for _, rawSub := range rawSubtitles {
			subMap, ok := rawSub.(map[string]any)
			if !ok {
				continue
			}

			subFileURL, _ := subMap["file"].(string)
			normalizedSubURL, err := c.normalizeMediaURL(subFileURL)
			if err != nil {
				continue
			}

			label, _ := subMap["title"].(string)
			language, _ := subMap["language"].(string)
			format, _ := subMap["format"].(string)
			if format == "" {
				format = "vtt"
			}
			isDefault, _ := subMap["default"].(bool)

			subtitles = append(subtitles, map[string]any{
				"url":      normalizedSubURL,
				"label":    label,
				"language": language,
				"format":   format,
				"default":  isDefault,
			})
		}
	}

	return &WatchResult{
		URL:          streamURL,
		Subtitles:    subtitles,
		SourceNumber: sourceNumber,
		Referer:      c.buildURL(watchPath),
	}, nil
}

// --------------------------------------------------
// SOURCES
// --------------------------------------------------

func (c *Client) Sources(ctx context.Context, slug string, episodeID string, language, quality *string) (map[string]any, error) {
	sourceNumber, err := c.getEpisodeSourceNumber(ctx, slug, episodeID)
	if err != nil {
		return nil, err
	}

	watch, err := c.scrapeWatch(ctx, slug, sourceNumber)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"provider":   "anizone",
		"anime_id":   slug,
		"episode_id": episodeID,
		"source_id":  strconv.Itoa(sourceNumber),
		"sources": []map[string]any{
			{
				"id":          strconv.Itoa(sourceNumber),
				"label":       "AniZone HLS",
				"type":        "hls",
				"url":         watch.URL,
				"quality":     nil,
				"language":    nil,
				"headers": map[string]string{
					"Referer":    watch.Referer,
					"User-Agent": c.userAgent,
				},
				"direct_file": false,
			},
		},
		"subtitles": watch.Subtitles,
	}, nil
}

// --------------------------------------------------
// RESOLVE
// --------------------------------------------------

func (c *Client) Resolve(ctx context.Context, slug string, episodeID string, sourceID *string, language, quality *string) (map[string]any, error) {
	sourceNumber, err := c.getEpisodeSourceNumber(ctx, slug, episodeID)
	if err != nil {
		return nil, err
	}

	if sourceID != nil && *sourceID != "" && *sourceID != strconv.Itoa(sourceNumber) {
		return nil, NewAniZoneError("Requested source does not match episode", "SOURCE_NOT_FOUND")
	}

	watch, err := c.scrapeWatch(ctx, slug, sourceNumber)
	if err != nil {
		return nil, err
	}

	var preferredSubtitle map[string]any
	if len(watch.Subtitles) > 0 {
		for _, s := range watch.Subtitles {
			if def, ok := s["default"].(bool); ok && def {
				preferredSubtitle = s
				break
			}
		}
		if preferredSubtitle == nil {
			preferredSubtitle = watch.Subtitles[0]
		}
	}

	return map[string]any{
		"provider":   "anizone",
		"anime_id":   slug,
		"episode_id": episodeID,
		"source_id":  strconv.Itoa(sourceNumber),
		"status":     "resolved",
		"available":  true,
		"source": map[string]any{
			"label": "AniZone HLS",
			"type":  "hls",
			"url":   watch.URL,
			"headers": map[string]string{
				"Referer":    watch.Referer,
				"User-Agent": c.userAgent,
			},
			"direct_file": false,
		},
		"subtitle":  preferredSubtitle,
		"subtitles": watch.Subtitles,
	}, nil
}

// --------------------------------------------------
// SUBTITLES
// --------------------------------------------------

func (c *Client) Subtitles(ctx context.Context, slug string, episodeID string) (map[string]any, error) {
	sourceNumber, err := c.getEpisodeSourceNumber(ctx, slug, episodeID)
	if err != nil {
		return nil, err
	}

	watch, err := c.scrapeWatch(ctx, slug, sourceNumber)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"provider":   "anizone",
		"anime_id":   slug,
		"episode_id": episodeID,
		"subtitles":  watch.Subtitles,
	}, nil
}

// --------------------------------------------------
// JAVASCRIPT / JSON EXTRACTION HELPERS
// --------------------------------------------------

func (c *Client) extractJSString(content, marker string) *string {
	start := strings.Index(content, marker)
	if start < 0 {
		return nil
	}
	start += len(marker)

	var sb strings.Builder
	escaped := false

	for i := start; i < len(content); i++ {
		char := content[i]
		if !escaped && char == '\'' {
			break
		}
		if escaped {
			sb.WriteByte('\\')
			sb.WriteByte(char)
			escaped = false
		} else if char == '\\' {
			escaped = true
		} else {
			sb.WriteByte(char)
		}
	}

	if escaped {
		sb.WriteByte('\\')
	}

	res := sb.String()
	return &res
}

func (c *Client) decodeJSString(raw string) string {
	var sb strings.Builder
	runes := []rune(raw)
	n := len(runes)
	i := 0

	for i < n {
		char := runes[i]
		if char != '\\' {
			sb.WriteRune(char)
			i++
			continue
		}

		i++
		if i >= n {
			break
		}

		char = runes[i]
		switch char {
		case 'n':
			sb.WriteRune('\n')
		case 'r':
			sb.WriteRune('\r')
		case 't':
			sb.WriteRune('\t')
		case 'b':
			sb.WriteRune('\b')
		case 'f':
			sb.WriteRune('\f')
		case 'v':
			sb.WriteRune('\v')
		case '0':
			sb.WriteRune('\x00')
		case 'u':
			if i+4 < n {
				codeHex := string(runes[i+1 : i+5])
				if code, err := strconv.ParseInt(codeHex, 16, 32); err == nil {
					sb.WriteRune(rune(code))
					i += 4
				} else {
					sb.WriteRune('u')
				}
			} else {
				sb.WriteRune('u')
			}
		case 'x':
			if i+2 < n {
				codeHex := string(runes[i+1 : i+3])
				if code, err := strconv.ParseInt(codeHex, 16, 32); err == nil {
					sb.WriteRune(rune(code))
					i += 2
				} else {
					sb.WriteRune('x')
				}
			} else {
				sb.WriteRune('x')
			}
		default:
			sb.WriteRune(char)
		}
		i++
	}

	return sb.String()
}

func (c *Client) jsonArgument(content, name string) any {
	marker := fmt.Sprintf("%s: JSON.parse('", name)
	raw := c.extractJSString(content, marker)
	if raw == nil {
		return nil
	}

	decoded := c.decodeJSString(*raw)
	var out any
	if err := json.Unmarshal([]byte(decoded), &out); err != nil {
		return nil
	}
	return out
}

func (c *Client) jsonCallArgument(content, name string) any {
	marker := fmt.Sprintf("%s(JSON.parse('", name)
	raw := c.extractJSString(content, marker)
	if raw == nil {
		return nil
	}

	decoded := c.decodeJSString(*raw)
	var out any
	if err := json.Unmarshal([]byte(decoded), &out); err != nil {
		return nil
	}
	return out
}

func (c *Client) normalizeMediaURL(val any) (string, error) {
	raw, ok := val.(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return "", NewAniZoneError("Player media URL is missing", "MEDIA_URL_MISSING")
	}

	raw = html.UnescapeString(strings.ReplaceAll(strings.TrimSpace(raw), `\/`, `/`))
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", NewAniZoneError("Invalid or unsupported media URL", "INVALID_MEDIA_URL")
	}

	return raw, nil
}

func (c *Client) pickTitle(val map[string]any) *string {
	if val == nil {
		return nil
	}

	for _, key := range []string{"1", "5", "8"} {
		if t, ok := val[key].(string); ok && strings.TrimSpace(t) != "" {
			res := strings.TrimSpace(t)
			return &res
		}
	}

	for _, v := range val {
		if t, ok := v.(string); ok && strings.TrimSpace(t) != "" {
			res := strings.TrimSpace(t)
			return &res
		}
	}

	return nil
}

var csrfRegex1 = regexp.MustCompile(`(?i)<meta[^>]+name=["']csrf-token["'][^>]+content=["']([^"']+)`)
var csrfRegex2 = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+name=["']csrf-token`)

func (c *Client) csrfToken(content string) string {
	if m := csrfRegex1.FindStringSubmatch(content); len(m) > 1 {
		return html.UnescapeString(m[1])
	}
	if m := csrfRegex2.FindStringSubmatch(content); len(m) > 1 {
		return html.UnescapeString(m[1])
	}
	return ""
}

func (c *Client) htmlAttributeBefore(content string, before int) string {
	if before > len(content) {
		before = len(content)
	}
	prefix := content[:before]
	marker := `wire:snapshot="`
	start := strings.LastIndex(prefix, marker)
	if start < 0 {
		return ""
	}

	start += len(marker)
	end := strings.Index(prefix[start:], `"`)
	if end < 0 {
		return ""
	}

	return html.UnescapeString(prefix[start : start+end])
}

func (c *Client) jsPropertyString(content, name string) *string {
	pattern := fmt.Sprintf(`\b%s\s*:\s*(['"])(.*?)\1`, regexp.QuoteMeta(name))
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(content)
	if len(m) > 2 {
		s := c.decodeJSString(m[2])
		return &s
	}
	return nil
}

func (c *Client) jsPropertyBool(content, name string) bool {
	pattern := fmt.Sprintf(`\b%s\s*:\s*(true|false)`, regexp.QuoteMeta(name))
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(content)
	if len(m) > 1 {
		return m[1] == "true"
	}
	return false
}

func (c *Client) mergeCookies(existing map[string]string, incoming []string) map[string]string {
	result := make(map[string]string)
	for k, v := range existing {
		result[k] = v
	}

	for _, cookie := range incoming {
		parts := strings.SplitN(cookie, "=", 2)
		if len(parts) == 2 {
			name := strings.TrimSpace(parts[0])
			rest := parts[1]
			val := strings.TrimSpace(strings.SplitN(rest, ";", 2)[0])
			result[name] = val
		}
	}

	return result
}

func (c *Client) cookieHeader(cookies map[string]string) string {
	pairs := make([]string, 0, len(cookies))
	for k, v := range cookies {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(pairs, "; ")
}

func (c *Client) validateSlug(slug string) error {
	if !slugRegex.MatchString(slug) {
		return NewAniZoneError("Invalid AniZone ID", "INVALID_ID")
	}
	return nil
}

func (c *Client) mergeItems(existing, incoming []map[string]any) []map[string]any {
	merged := make([]map[string]any, len(existing))
	copy(merged, existing)

	identity := func(item map[string]any) string {
		if s, ok := item["slug"].(string); ok && s != "" {
			return s
		}
		if u, ok := item["url"].(string); ok && u != "" {
			return u
		}
		return ""
	}

	seen := make(map[string]bool)
	for _, it := range merged {
		id := identity(it)
		if id != "" {
			seen[id] = true
		}
	}

	for _, it := range incoming {
		id := identity(it)
		if id == "" || !seen[id] {
			merged = append(merged, it)
			if id != "" {
				seen[id] = true
			}
		}
	}

	return merged
}

func (c *Client) Metadata() map[string]any {
	return map[string]any{
		"id":       "anizone",
		"name":     "AniZone",
		"enabled":  config.Settings.AniZoneEnabled,
		"base_url": c.baseURL,
		"capabilities": []string{
			"search",
			"details",
			"episodes",
			"sources",
			"resolve",
			"subtitles",
		},
	}
}
