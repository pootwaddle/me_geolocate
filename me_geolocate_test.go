package me_geolocate

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	// Ensure Redis is enabled for the test
	os.Setenv("REDIS_CONF", "localhost:6379")
	os.Exit(m.Run())
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
	assert.True(t, geo.isLocal())
	assert.Equal(t, "LaughingJ", geo.ISP)
	assert.Equal(t, Blue+"LaughingJ"+Reset, geo.CacheHit)
}

func TestCheckRedisCache(t *testing.T) {
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
	redisClient.Set(ctx, mockIP, jsonVal, 0)

	geo := GeoIPData{IP: mockIP}
	hit := geo.checkRedisCache(redisClient, mockIP)
	assert.True(t, hit)
	assert.Equal(t, Green+"true"+Reset, geo.CacheHit)
}

func TestGetGeoData_CacheHit(t *testing.T) {
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
	redisClient.Set(ctx, mockIP, jsonVal, 0)

	geo := GetGeoData(mockIP)
	assert.Equal(t, Green+"true"+Reset, geo.CacheHit)
	assert.Equal(t, "US", geo.CountryCode)
	assert.Equal(t, "Google", geo.ISP)
}

func TestGetGeoData_NonRoutable(t *testing.T) {
	geo := GetGeoData("192.168.1.1")
	assert.Equal(t, BrightMagenta+"non-routable"+Reset, geo.CacheHit)
}
