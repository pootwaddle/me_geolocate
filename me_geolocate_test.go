package me_geolocate

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
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	loc, err := NewGeoLocator(logger)
	if err != nil {
		t.Fatalf("failed to init GeoLocator: %v", err)
	}
	return loc
}

func TestIsLocal(t *testing.T) {
	geo := GeoIPData{IP: "192.168.106.15"}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	assert.True(t, geo.IsLocal(logger))
	assert.Equal(t, "LaughingJ", geo.ISP)
	assert.Equal(t, "US", geo.CountryCode)
	assert.Equal(t, "Lewisville", geo.City)
	assert.Equal(t, "local", geo.IPClass)
}

func TestIsNonRoutable(t *testing.T) {
	cases := []struct {
		ip       string
		expected bool
		ipClass  string
	}{
		{"192.168.1.1", true, "non-routable"},
		{"10.0.0.1", true, "non-routable"},
		{"172.16.5.5", true, "non-routable"},
		{"8.8.8.8", false, ""},
		{"1.1.1.1", false, ""},
		{"192.168.106.15", false, "local"},
	}
	for _, tc := range cases {
		geo := GeoIPData{IP: tc.ip}
		geo.IsLocal(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
		res := geo.IsNonRoutable()
		assert.Equal(t, tc.expected, res, "Failed for IP: %s", tc.ip)
		if tc.ipClass != "" {
			assert.Equal(t, tc.ipClass, geo.IPClass, "Wrong ip_class for IP: %s", tc.ip)
		}
	}
}

func TestCheckRedisCache(t *testing.T) {
	loc := newTestGeoLocator(t)
	ctx := context.Background()
	mockIP := "8.8.8.8"

	mockData := GeoIPData{
		IP:          mockIP,
		ISP:         "Google",
		City:        "Mountain View",
		CountryCode: "US",
		CountryName: "United States",
		Success:     true,
		IPClass:     "cache_hit",
	}

	jsonVal, _ := json.Marshal(mockData)
	if err := loc.redis.Set(ctx, mockIP, jsonVal, 1*time.Minute).Err(); err != nil {
		t.Fatalf("redis Set failed: %v", err)
	}

	geo := GeoIPData{IP: mockIP}
	hit := loc.checkRedisCache(ctx, &geo)
	assert.True(t, hit)
	assert.Equal(t, "cache_hit", geo.IPClass)
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
		City:        "Mountain View",
		CountryCode: "US",
		CountryName: "United States",
		Success:     true,
		IPClass:     "cache_hit",
	}

	jsonVal, _ := json.Marshal(mockData)
	if err := loc.redis.Set(ctx, mockIP, jsonVal, 1*time.Minute).Err(); err != nil {
		t.Fatalf("redis Set failed: %v", err)
	}

	geo, err := loc.GetGeoData(ctx, mockIP)
	assert.NoError(t, err)
	assert.Equal(t, "cache_hit", geo.IPClass)
	assert.Equal(t, "US", geo.CountryCode)
	assert.Equal(t, "Google", geo.ISP)
}

func TestGetGeoData_NonRoutable(t *testing.T) {
	loc := newTestGeoLocator(t)
	ctx := context.Background()
	geo, err := loc.GetGeoData(ctx, "192.168.1.1")
	assert.NoError(t, err)
	assert.Equal(t, "non-routable", geo.IPClass)
}

func TestGetGeoData_Local(t *testing.T) {
	loc := newTestGeoLocator(t)
	ctx := context.Background()
	geo, err := loc.GetGeoData(ctx, "192.168.106.22")
	assert.NoError(t, err)
	assert.Equal(t, "local", geo.IPClass)
	assert.Equal(t, "Lewisville", geo.City)
}
