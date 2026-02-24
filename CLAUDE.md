# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Overview

**me_geolocate** is an IP geolocation library with Redis caching. It looks up geographic data (city, country, ISP) for IP addresses using the `json.geoiplookup.io` API and caches results in Redis for 180 days. Used by `websvc`, `geosvc`, `me_cleanit`, `me_logparse`, `me_spamparse`, and `me_add2block`.

**Module**: `github.com/pootwaddle/me_geolocate`
**Type**: Library
**Dependencies**: `github.com/go-redis/redis/v8`

## Exported API

### Types
```go
type GeoLocator struct { ... }  // Main service (Redis client + TTL + logger)

type GeoIPData struct {
    IP          string `json:"ip"`
    ISP         string `json:"isp"`
    City        string `json:"city"`
    CountryCode string `json:"country_code"`
    CountryName string `json:"country_name"`
    Success     bool   `json:"success"`
    Error       string `json:"error"`
    IPClass     string `json:"ip_class"`  // "local", "non-routable", "cache_hit", "cache_miss"
}
```

### Functions
- `NewGeoLocator(logger *slog.Logger) (*GeoLocator, error)` - Create a new GeoLocator with Redis connection
- `(g *GeoLocator) GetGeoData(ctx, ip) (GeoIPData, error)` - Lookup IP with caching

### IP Classification
1. **local** - `192.168.106.*` (LaughingJ network) - hardcoded response
2. **non-routable** - RFC1918 addresses (192.168.*, 10.*, 172.16-31.*) - returns error
3. **cache_hit** - Found in Redis
4. **cache_miss** - Fetched from `json.geoiplookup.io` and cached

## Configuration

- `REDIS_CONF` env var: Redis address (default `127.0.0.1:6379`)
- Cache TTL: 180 days (hardcoded)
- API: `https://json.geoiplookup.io/{ip}` (supports gzip)

## Build Commands

```powershell
go test
go build
```

## Important Notes

- Library module: use `go_update.ps1 -IsLibrary` for version tagging.
- Requires a running Redis instance.
- Has test files using `github.com/stretchr/testify`.
- Constructor requires a `*slog.Logger` - obtain via `slogger.GetLogger()`.
