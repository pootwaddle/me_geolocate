package me_geolocate

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

// ======= Types =======

type GeoLocator struct {
	redis  *redis.Client
	ttl    time.Duration
	logger *slog.Logger
}

type GeoIPData struct {
	IP          string `json:"ip"`
	ISP         string `json:"isp"`
	City        string `json:"city"`
	CountryCode string `json:"country_code"`
	CountryName string `json:"country_name"`
	Success     bool   `json:"success"`
	Error       string `json:"error"`
	IPClass     string `json:"ip_class"`
}

// ======= Constants =======

var (
	nonRoutableNet = []string{
		"192.168.", "10.",
		"172.16.", "172.17.", "172.18.", "172.19.",
		"172.20.", "172.21.", "172.22.", "172.23.",
		"172.24.", "172.25.", "172.26.", "172.27.",
		"172.28.", "172.29.", "172.30.", "172.31.",
	}
	colorBlue          = "\033[34m"
	colorBrightMagenta = "\033[95m"
	colorGreen         = "\033[32m"
	colorRed           = "\033[31m"
	colorReset         = "\033[0m"
)

// ======= Constructor =======

func NewGeoLocator(logger *slog.Logger) (*GeoLocator, error) {
	redisAddr := os.Getenv("REDIS_CONF")
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379"
	}
	logger.Info("👋 GeoLocator initializing", "redis_addr", redisAddr)

	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
		DB:   0,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &GeoLocator{
		redis:  rdb,
		ttl:    180 * 24 * time.Hour, // optionally make this configurable later
		logger: logger,
	}, nil
}

// ======= Public API =======

// GetGeoData retrieves geo data for an IP, using Redis cache, with context.
func (g *GeoLocator) GetGeoData(ctx context.Context, ip string) (GeoIPData, error) {
	geo := GeoIPData{
		IP:          ip,
		ISP:         "-----",
		City:        "-----",
		CountryCode: "--",
		CountryName: "-----",
	}

	// Check for local IP
	geo.checkOctets("112")
	if geo.IsLocal(g.logger) {
		g.logGeo(&geo)
		return geo, nil
	}
	if geo.IsNonRoutable() {
		g.logGeo(&geo)
		return geo, nil
	}

	// Try cache
	if g.checkRedisCache(ctx, &geo) && geo.CountryCode != "--" {
		geo.IPClass = "cache_hit"
		g.logGeo(&geo)
		return geo, nil
	}

	// Remote fetch
	if err := geo.obtainGeoDat(ctx, g.logger); err != nil {
		geo.Error = err.Error()
	}
	g.add2RedisCache(ctx, &geo)
	g.logGeo(&geo)
	return geo, nil
}

// ======= Redis Cache Methods =======

func (g *GeoLocator) checkRedisCache(ctx context.Context, geo *GeoIPData) bool {
	val, err := g.redis.Get(ctx, geo.IP).Result()
	if err == redis.Nil || err != nil {
		geo.IPClass = "cache_miss"
		return false
	}
	if err := json.Unmarshal([]byte(val), geo); err != nil {
		g.logger.Error("unmarshal Redis", "ip", geo.IP, "err", err)
		geo.IPClass = "cache_miss"
		return false
	}
	geo.IPClass = "cache_hit"
	return true
}

func (g *GeoLocator) add2RedisCache(ctx context.Context, geo *GeoIPData) {
	geo.IPClass = "cache_miss" // just being explicit
	b, err := json.Marshal(geo)
	if err != nil {
		g.logger.Error("marshal for Redis", "ip", geo.IP, "err", err)
		return
	}
	if err := g.redis.Set(ctx, geo.IP, b, g.ttl).Err(); err != nil {
		g.logger.Error("redis Set failed", "ip", geo.IP, "err", err)
	}
}

// ======= Internal/Helper Methods =======

func (geo *GeoIPData) checkOctets(o string) {
	octets := strings.Split(geo.IP, ".")
	if len(octets) == 3 {
		geo.IP = octets[0] + "." + octets[1] + "." + octets[2] + "." + o
	}
}

func (geo *GeoIPData) IsLocal(logger *slog.Logger) bool {
	if strings.HasPrefix(geo.IP, "192.168.106.") {
		geo.ISP = "LaughingJ"
		geo.CountryCode = "US"
		geo.City = "Lewisville"
		geo.CountryName = "United States"
		geo.IPClass = "local"
		geo.Success = true
		logger.Info("🔵 detected local IP", "ip", geo.IP)
		return true
	}
	return false
}

func (geo *GeoIPData) IsNonRoutable() bool {
	// Only mark as "non-routable" if not "local"
	if geo.IPClass == "local" {
		return false
	}
	for _, v := range nonRoutableNet {
		if strings.HasPrefix(geo.IP, v) {
			geo.Success = false
			geo.IPClass = "non-routable"
			geo.Error = fmt.Sprintf("Invalid public IPv4 or IPv6 address %s", geo.IP)
			return true
		}
	}
	return false
}

func (geo *GeoIPData) obtainGeoDat(ctx context.Context, logger *slog.Logger) error {
	url := fmt.Sprintf("https://json.geoiplookup.io/%s", geo.IP)

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Accept-Encoding", "gzip")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Error("HTTP request failed", "ip", geo.IP, "err", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		geo.Error = fmt.Sprintf("Invalid response %d from geoip service", resp.StatusCode)
		return errors.New(geo.Error)
	}

	var reader io.ReadCloser
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		reader, _ = gzip.NewReader(resp.Body)
	default:
		reader = resp.Body
	}
	defer reader.Close()

	b, err := io.ReadAll(reader)
	if err != nil {
		geo.Error = fmt.Sprintf("Reading response body failed - %s", err)
		return err
	}

	if err := json.Unmarshal(b, geo); err != nil {
		logger.Error("Unmarshal failed", "ip", geo.IP, "err", err)
		return err
	}
	logger.Debug("parsed geo answer", "ip", geo.IP, "geo", geo)
	return nil
}

// ======= Logging Helpers =========

func (g *GeoLocator) logGeo(geo *GeoIPData) {
	emoji := geo.PrintColorStatus() // Always print, always color, give us a corresponding emoji

	g.logger.Info(emoji+" GeoIP result",
		slog.String("ip", geo.IP),
		slog.String("ip_class", geo.IPClass),
		slog.String("country_code", geo.CountryCode),
		slog.String("city", geo.City),
		slog.String("country_name", geo.CountryName),
		slog.String("isp", geo.ISP),
	)
}

func (geo *GeoIPData) PrintColorStatus() string {
	var color string
	var emoji string
	switch geo.IPClass {
	case "cache_hit":
		color = colorGreen
		emoji = "✔️" // check mark — well-supported and visually clear
	case "cache_miss":
		color = colorRed
		emoji = "❌" // red X — shows failure clearly
	case "non-routable":
		color = colorBrightMagenta
		emoji = "🚫" // forbidden / blocked
	case "local":
		color = colorBlue
		emoji = "🔵" // house for local IPs
	default:
		color = colorReset
		emoji = "❓" // fallback for unknowns
	}
	fmt.Printf("GeoIP [%s%s%s]: %s, %s | ISP: %s\n",
		color, geo.IP, colorReset, geo.CountryCode, geo.City, geo.ISP)
	return emoji
}
