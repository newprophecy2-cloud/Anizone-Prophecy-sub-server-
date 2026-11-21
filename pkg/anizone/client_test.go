package anizone

import (
	"strings"
	"testing"
	"time"

	"github.com/newprophecy2-cloud/Anizone-Prophecy-sub-server-/pkg/config"
)

func TestParseSearchItems(t *testing.T) {
	cfg := &config.Config{
		AniZoneBaseURL: "https://anizone.to",
		RequestTimeout: 5 * time.Second,
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	items := []map[string]any{
		{
			"slug": "naruto-shippuden",
			"title_list": map[string]any{
				"1": "Naruto Shippuden",
			},
			"type":          "series",
			"start_year":    2007.0,
			"cover":         "https://example.com/cover.jpg",
			"episode_count": 500.0,
		},
		{
			"slug":       "one-piece-movie-1",
			"main_title": "One Piece: The Movie",
			"type":       "movie",
		},
	}

	results := client.parseSearchItems(items)
	if len(results) != 2 {
		t.Fatalf("Expected 2 search items, got %d", len(results))
	}

	if results[0].Slug != "naruto-shippuden" || results[0].Title != "Naruto Shippuden" || results[0].MediaType != "series" {
		t.Errorf("Unexpected first item: %+v", results[0])
	}
	if *results[0].Year != 2007 || *results[0].EpisodeCount != 500 {
		t.Errorf("Unexpected year or count: %+v", results[0])
	}

	if results[1].Slug != "one-piece-movie-1" || results[1].Title != "One Piece: The Movie" || results[1].MediaType != "movie" {
		t.Errorf("Unexpected second item: %+v", results[1])
	}
}

func TestParseEpisodes(t *testing.T) {
	cfg := &config.Config{
		AniZoneBaseURL: "https://anizone.to",
		RequestTimeout: 5 * time.Second,
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	items := []map[string]any{
		{
			"slug": "2",
			"title_list": map[string]any{
				"1": "Episode 2",
			},
			"season":  1,
			"summary": "Episode 2 summary",
		},
		{
			"url": "/anime/naruto/1",
			"title_list": map[string]any{
				"1": "Episode 1",
			},
			"season": 1,
		},
	}

	episodes := client.parseEpisodes(items)
	if len(episodes) != 2 {
		t.Fatalf("Expected 2 episodes, got %d", len(episodes))
	}

	if episodes[0].Number != 1 || episodes[1].Number != 2 {
		t.Errorf("Episodes not sorted properly: %+v", episodes)
	}
}

func TestDecodeJSString(t *testing.T) {
	cfg := &config.Config{
		AniZoneBaseURL: "https://anizone.to",
	}
	client, _ := NewClient(cfg)

	raw := `Hello\\nWorld\u0020Test`
	decoded := client.decodeJSString(raw)
	if !strings.Contains(decoded, "Hello") || !strings.Contains(decoded, "Test") {
		t.Errorf("Unexpected decoded string: %s", decoded)
	}
}

func TestValidateSlug(t *testing.T) {
	cfg := &config.Config{
		AniZoneBaseURL: "https://anizone.to",
	}
	client, _ := NewClient(cfg)

	if err := client.validateSlug("valid-slug-123"); err != nil {
		t.Errorf("Expected valid slug, got error: %v", err)
	}

	if err := client.validateSlug("invalid slug!"); err == nil {
		t.Errorf("Expected error for invalid slug, got nil")
	}
}

func TestJsPropertyString(t *testing.T) {
	cfg := &config.Config{
		AniZoneBaseURL: "https://anizone.to",
	}
	client, _ := NewClient(cfg)

	got := client.jsPropertyString(`{ nextCursor: 'abc123', hasMore: true }`, "nextCursor")
	if got == nil || *got != "abc123" {
		t.Fatalf("Expected abc123, got %v", got)
	}

	got2 := client.jsPropertyString(`{ nextCursor: "xyz\"789", hasMore: false }`, "nextCursor")
	if got2 == nil || *got2 != `xyz"789` {
		t.Fatalf("Expected xyz\"789, got %v", got2)
	}
}
