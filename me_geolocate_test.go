package megeolocate

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Helper: Create a test logger and locator
func newTestGeoLocator(t *testing.T) *GeoLocator {
	redisAddr := "localhost:6379"
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	loc, err := NewGeoLocator(redisAddr, 1, logger) // 1 minute TTL for tests
	if err != nil {
		t.Fatalf("failed to init GeoLocator: %v", err)
	}
	return loc
}

func TestIsNonRoutable(t *testing.T) {
	cases := []struct {
		ip       string
		expected bool
	}{
		{"192.168.1.1", true},
		{"10.0.0.1", true},
		{"172.16.5.5", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
	}
	for _, tc := range cases {
		geo := GeoIPData{IP: tc.ip}
		assert.Equal(t, tc.expected, geo.isNonRoutable(), "Failed for IP: %s", tc.ip)
	}
}

func TestIsLocal(t *testing.T) {
	geo := GeoIPData{IP: "192.168.106.15"}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	assert.True(t, geo.isLocal(logger))
	assert.Equal(t, "LaughingJ", geo.ISP)
	assert.Equal(t, false, geo.CacheHit)
	assert.Equal(t, "US", geo.CountryCode)
	assert.Equal(t, "Lewisville", geo.City)
}

func TestCheckRedisCache(t *testing.T) {
	loc := newTestGeoLocator(t)
	ctx := context.Background()
	mockIP := "8.8.8.8"

	mockData := GeoIPData{
		IP:          mockIP,
		ISP:         "Google",
		Org:         "Google LLC",
		Hostname:    "dns.google",
		City:        "Mountain View",
		CountryCode: "US",
		CountryName: "United States",
		Success:     true,
		Located:     true,
		Routable:    true,
	}

	jsonVal, _ := json.Marshal(mockData)
	if err := loc.redis.Set(ctx, mockIP, jsonVal, 1*time.Minute).Err(); err != nil {
		t.Fatalf("redis Set failed: %v", err)
	}

	geo := GeoIPData{IP: mockIP}
	hit := loc.checkRedisCache(ctx, &geo)
	assert.True(t, hit)
	assert.True(t, geo.CacheHit)
	assert.Equal(t, "US", geo.CountryCode)
	assert.Equal(t, "Google", geo.ISP)
}

func TestGetGeoData_CacheHit(t *testing.T) {
	loc := newTestGeoLocator(t)
	ctx := context.Background()
	mockIP := "8.8.8.8"

	mockData := GeoIPData{
		IP:          mockIP,
		ISP:         "Google",
		Org:         "Google LLC",
		Hostname:    "dns.google",
		City:        "Mountain View",
		CountryCode: "US",
		CountryName: "United States",
		Success:     true,
		Located:     true,
		Routable:    true,
	}

	jsonVal, _ := json.Marshal(mockData)
	if err := loc.redis.Set(ctx, mockIP, jsonVal, 1*time.Minute).Err(); err != nil {
		t.Fatalf("redis Set failed: %v", err)
	}

	geo, err := loc.GetGeoData(ctx, mockIP)
	assert.NoError(t, err)
	assert.True(t, geo.CacheHit)
	assert.Equal(t, "US", geo.CountryCode)
	assert.Equal(t, "Google", geo.ISP)
}

func TestGetGeoData_NonRoutable(t *testing.T) {
	loc := newTestGeoLocator(t)
	ctx := context.Background()
	geo, err := loc.GetGeoData(ctx, "192.168.1.1")
	assert.NoError(t, err)
	assert.False(t, geo.CacheHit)
	assert.False(t, geo.Routable)
	assert.False(t, geo.Located)
}
