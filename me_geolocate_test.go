package me_geolocate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
	assert.Equal(t, true, geo.CacheHit)
}

func TestCheckRedisCache(t *testing.T) {
	ctx := context.Background()
	mockIP := "8.8.8.8"
	geo := GeoIPData{IP: mockIP}
	redisClient.Set(ctx, mockIP, `{"ip": "8.8.8.8", "isp": "Google", "located": true}`, 0)

	hit := geo.checkRedisCache(redisClient, mockIP)
	assert.True(t, hit)
	assert.Equal(t, true, geo.CacheHit)
}

func TestGetGeoData_CacheHit(t *testing.T) {
	ctx := context.Background()
	mockIP := "8.8.8.8"
	redisClient.Set(ctx, mockIP, `{"ip": "8.8.8.8", "isp": "Google", "located": true}`, 0)

	geo := GetGeoData(mockIP)
	assert.Equal(t, true, geo.CacheHit)
}

func TestGetGeoData_NonRoutable(t *testing.T) {
	geo := GetGeoData("192.168.1.1")
	assert.Equal(t, false, geo.CacheHit)
}
