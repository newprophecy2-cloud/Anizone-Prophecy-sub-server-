package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/newprophecy2-cloud/Anizone-Prophecy-sub-server-/pkg/config"
	"github.com/newprophecy2-cloud/Anizone-Prophecy-sub-server-/pkg/engine"
)

type Server struct {
	cfg    *config.Config
	engine *engine.ProviderEngine
	mux    *http.ServeMux
}

type ExecuteRequest struct {
	Action string         `json:"action"`
	Params map[string]any `json:"params"`
}

type ResolveRequest struct {
	SourceID string  `json:"source_id"`
	Language *string `json:"language,omitempty"`
	Quality  *string `json:"quality,omitempty"`
}

func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func (s *Server) registerRoutes() {
	mux := http.NewServeMux()

	// Root & Health
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /health", s.handleHealth)

	// Manifest & Providers
	mux.HandleFunc("GET /api/v1/manifest", s.handleManifest)
	mux.HandleFunc("GET /api/v1/providers", s.handleListProviders)
	mux.HandleFunc("GET /api/v1/providers/{provider_id}", s.handleProviderDetails)
	mux.HandleFunc("POST /api/v1/providers/{provider_id}/execute", s.handleExecuteProvider)

	// Direct Anime routes
	mux.HandleFunc("GET /api/v1/anime/search", s.handleSearchAnime)
	mux.HandleFunc("GET /api/v1/anime/{slug}", s.handleAnimeDetails)
	mux.HandleFunc("GET /api/v1/anime/{slug}/episodes", s.handleAnimeEpisodes)
	mux.HandleFunc("GET /api/v1/anime/{slug}/episodes/{episode_id}/sources", s.handleEpisodeSources)
	mux.HandleFunc("POST /api/v1/anime/{slug}/episodes/{episode_id}/resolve", s.handleResolveEpisodeSource)
	mux.HandleFunc("GET /api/v1/anime/{slug}/episodes/{episode_id}/subtitles", s.handleEpisodeSubtitles)

	s.mux = mux
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, map[string]any{
		"name":     s.cfg.AppName,
		"status":   "online",
		"version":  s.cfg.AppVersion,
		"docs":     "/docs",
		"health":   "/health",
		"manifest": "/api/v1/manifest",
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, map[string]any{
		"status":           "ok",
		"service":          "prophecy-anizone",
		"provider_enabled": s.cfg.AniZoneEnabled,
	})
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, map[string]any{
		"name":               s.cfg.AppName,
		"version":            s.cfg.AppVersion,
		"protocol":           "prophecy-provider-v1",
		"status":             "ready",
		"providers_endpoint": "/api/v1/providers",
		"execute_endpoint":   "/api/v1/providers/{provider_id}/execute",
		"providers":          s.engine.ListProviders(),
		"capabilities": []string{
			"search",
			"details",
			"episodes",
			"sources",
			"resolve",
			"subtitles",
		},
	})
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"providers": s.engine.ListProviders(),
	})
}

func (s *Server) handleProviderDetails(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider_id")
	meta := s.engine.GetProvider(providerID)
	if meta == nil {
		jsonResponse(w, http.StatusNotFound, map[string]any{
			"detail": map[string]any{
				"code":     "PROVIDER_NOT_FOUND",
				"provider": providerID,
			},
		})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"provider": meta,
	})
}

func (s *Server) handleExecuteProvider(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider_id")

	var req ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, s.engine.ErrorResponse("INVALID_PARAMETERS", "Invalid JSON payload", providerID, "", false))
		return
	}

	if req.Action == "" || len(req.Action) > 30 {
		jsonResponse(w, http.StatusBadRequest, s.engine.ErrorResponse("INVALID_PARAMETERS", "Action length must be between 1 and 30 characters", providerID, req.Action, false))
		return
	}

	result := s.engine.Execute(r.Context(), providerID, req.Action, req.Params)
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) handleSearchAnime(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if len(q) < 1 || len(q) > 150 {
		jsonResponse(w, http.StatusBadRequest, map[string]any{
			"detail": map[string]any{
				"code":    "INVALID_QUERY",
				"message": "Query must be between 1 and 150 characters",
			},
		})
		return
	}

	pageStr := r.URL.Query().Get("page")
	page := 1
	if pageStr != "" {
		p, err := strconv.Atoi(pageStr)
		if err != nil || p < 1 {
			jsonResponse(w, http.StatusBadRequest, map[string]any{
				"detail": map[string]any{
					"code":    "INVALID_PAGE",
					"message": "Page must be greater than or equal to 1",
				},
			})
			return
		}
		page = p
	}

	if page > s.cfg.MaxPaginationPages {
		jsonResponse(w, http.StatusBadRequest, map[string]any{
			"detail": map[string]any{
				"code":    "INVALID_PAGE",
				"message": "Page exceeds configured limit.",
			},
		})
		return
	}

	result := s.engine.Execute(r.Context(), "anizone", "search", map[string]any{
		"query": q,
		"page":  page,
	})
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) handleAnimeDetails(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	result := s.engine.Execute(r.Context(), "anizone", "details", map[string]any{
		"slug": slug,
	})
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) handleAnimeEpisodes(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	result := s.engine.Execute(r.Context(), "anizone", "episodes", map[string]any{
		"slug": slug,
	})
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) handleEpisodeSources(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	episodeID := r.PathValue("episode_id")
	language := r.URL.Query().Get("language")

	params := map[string]any{
		"slug":       slug,
		"episode_id": episodeID,
	}
	if language != "" {
		params["language"] = language
	}

	result := s.engine.Execute(r.Context(), "anizone", "sources", params)
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) handleResolveEpisodeSource(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	episodeID := r.PathValue("episode_id")

	var req ResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, http.StatusBadRequest, s.engine.ErrorResponse("INVALID_PARAMETERS", "Invalid JSON body", "anizone", "resolve", false))
		return
	}

	if req.SourceID == "" || len(req.SourceID) > 300 {
		jsonResponse(w, http.StatusBadRequest, s.engine.ErrorResponse("INVALID_PARAMETERS", "source_id must be between 1 and 300 characters", "anizone", "resolve", false))
		return
	}

	params := map[string]any{
		"slug":       slug,
		"episode_id": episodeID,
		"source_id":  req.SourceID,
	}
	if req.Language != nil {
		params["language"] = *req.Language
	}
	if req.Quality != nil {
		params["quality"] = *req.Quality
	}

	result := s.engine.Execute(r.Context(), "anizone", "resolve", params)
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) handleEpisodeSubtitles(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	episodeID := r.PathValue("episode_id")

	result := s.engine.Execute(r.Context(), "anizone", "subtitles", map[string]any{
		"slug":       slug,
		"episode_id": episodeID,
	})
	jsonResponse(w, http.StatusOK, result)
}

func main() {
	cfg := config.Load()

	eng, err := engine.NewProviderEngine(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize provider engine: %v", err)
	}
	defer eng.Close()

	srv := &Server{
		cfg:    cfg,
		engine: eng,
	}
	srv.registerRoutes()

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      srv.mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Starting %s v%s on http://%s", cfg.AppName, cfg.AppVersion, addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-stopChan
	log.Println("Shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
	log.Println("Server stopped")
}
