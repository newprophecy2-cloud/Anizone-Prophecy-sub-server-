package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppName            string
	AppVersion         string
	AppEnv             string
	Host               string
	Port               int
	AniZoneEnabled     bool
	AniZoneBaseURL     string
	RequestTimeout     time.Duration
	RequestRetries     int
	UserAgent          string
	CustomCookie       string
	ProxyURL           string
	MaxResponseBytes   int64
	MaxPaginationPages int
}

var Settings = Load()

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	v := strings.ToLower(strings.TrimSpace(val))
	return v == "true" || v == "1" || v == "yes"
}

func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
		return n
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
		return f
	}
	return defaultVal
}

func Load() *Config {
	port := getEnvInt("PORT", 8000)
	timeoutSec := getEnvFloat("REQUEST_TIMEOUT", 20.0)

	baseURL := getEnv("ANIZONE_BASE_URL", "https://anizone.to")
	baseURL = strings.TrimRight(baseURL, "/")

	return &Config{
		AppName:            getEnv("APP_NAME", "Prophecy AniZone Sub Server"),
		AppVersion:         getEnv("APP_VERSION", "1.0.0"),
		AppEnv:             getEnv("APP_ENV", "production"),
		Host:               getEnv("HOST", "0.0.0.0"),
		Port:               port,
		AniZoneEnabled:     getEnvBool("ANIZONE_ENABLED", true),
		AniZoneBaseURL:     baseURL,
		RequestTimeout:     time.Duration(timeoutSec * float64(time.Second)),
		RequestRetries:     getEnvInt("REQUEST_RETRIES", 2),
		UserAgent:          getEnv("USER_AGENT", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"),
		CustomCookie:       getEnv("CUSTOM_COOKIE", ""),
		ProxyURL:           getEnv("HTTP_PROXY", getEnv("HTTPS_PROXY", "")),
		MaxResponseBytes:   int64(getEnvInt("MAX_RESPONSE_BYTES", 4*1024*1024)),
		MaxPaginationPages: getEnvInt("MAX_PAGINATION_PAGES", 64),
	}
}
