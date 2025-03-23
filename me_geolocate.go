// Package geolocate handles the lookup of geo IP data from https://geoiplookup.io/api
// It looks first in the Redis cache
// And finally, on a miss to cache, it makes a call to
// https://json.geoiplookup.io/8.8.8.8 for the data and adds the data to the cache for next time
// entrypoint: func GetGeoData(ip string) returns  GeoIPData struct
package me_geolocate

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/romana/rlog"
)

const (
	Red           = "\033[31m"
	Green         = "\033[32m"
	Blue          = "\033[34m"
	BrightMagenta = "\033[95m"
	Reset         = "\033[0m"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func ColorForIP(geo *GeoIPData) string {
	switch {
	case strings.HasPrefix(geo.IP, "192.168.106."):
		return Blue // Local IPs
	case !geo.Routable:
		return BrightMagenta // Non-routable
	case geo.CacheHit:
		return Green // Cache hit
	default:
		return Red // API fallback
	}
}

func LogGeo(geo *GeoIPData) {
	// Choose color based on IP type
	color := ColorForIP(geo)
	coloredIP := fmt.Sprintf("%s%s%s", color, geo.IP, Reset)

	// Console output
	fmt.Printf("{IP:%s, CC:%s, Hit:%t}\n", coloredIP, geo.CountryCode, geo.CacheHit)

	// Log clean struct
	jsonLog, err := json.Marshal(geo)
	if err != nil {
		rlog.Errorf("Failed to marshal GeoIPData for log: %s", err)
		return
	}

	rlog.Info(string(jsonLog))
}

// https://json.geoiplookup.io/8.8.8.8
// seems up-to-date.   Limit 500 lookups per hour
type GeoIPData struct {
	IP          string `json:"ip"`
	ISP         string `json:"isp"`
	Org         string `json:"org"`
	Hostname    string `json:"hostname"`
	City        string `json:"city"`
	CountryCode string `json:"country_code"`
	CountryName string `json:"country_name"`
	Success     bool   `json:"success"`
	Error       string `json:"error"`
	Located     bool   `json:"located"`
	Routable    bool   `json:"routable"`
	Block       bool   `json:"block"`
	CacheHit    bool   `json:"cache_hit"` // used to indicate if we found the IP in the Redis cache
}

const ttl int = 129600 // 90 days in minutes  60*24*90
var redisClient *redis.Client
var redis_addr string

func init() {
	redis_addr = os.Getenv("REDIS_CONF")
	var ctx = context.Background()
	redisClient = redis.NewClient(&redis.Options{
		Addr:     redis_addr,
		Password: "",
		DB:       0,
	})
	pong, err := redisClient.Ping(ctx).Result()
	if err != nil {
		//do something - probably set environment variable
	}
	rlog.Printf("%s", pong)
}

func (g *GeoIPData) checkRedisCache(redisClient *redis.Client, ip string) bool {
	ctx := context.Background()

	jsonResult, err := redisClient.Get(ctx, ip).Result()
	if err == redis.Nil || err != nil {
		g.Located = false
		g.CacheHit = false
		return false
	}

	if err := json.Unmarshal([]byte(jsonResult), g); err != nil {
		rlog.Errorf("Error unmarshaling Redis value for %s: %s", ip, err)
		g.Located = false
		g.CacheHit = false
		return false
	}

	g.Located = true
	g.CacheHit = true
	return true
}

func (g *GeoIPData) add2RedisCache(redisClient *redis.Client, minutes int) {
	ttl := time.Duration(time.Minute * time.Duration(minutes))
	ctx := context.Background()

	// Mark as a cache miss until this is picked up again
	g.CacheHit = false

	jsonResult, err := json.Marshal(g)
	if err != nil {
		rlog.Errorf("Error marshaling GeoIPData for Redis: %s", err)
		return
	}

	err = redisClient.Set(ctx, g.IP, jsonResult, ttl).Err()
	if err != nil {
		rlog.Errorf("Error adding to Redis Cache for IP %s: %s", g.IP, err)
	}
}

func (g *GeoIPData) CheckOctets(o string) {
	octets := strings.Split(g.IP, ".")
	if len(octets) == 3 {
		g.IP = octets[0] + "." + octets[1] + "." + octets[2] + "." + o
	}
}

// GetGeoData Entrypoint - initializes a search for the geoLocation of an IP.
func GetGeoData(ip string) GeoIPData {
	geo := GeoIPData{
		IP:          ip,
		ISP:         "-----",
		Hostname:    "-----",
		City:        "-----",
		CountryCode: "--",
		CountryName: "-----",
		Located:     false,
		Routable:    false,
		CacheHit:    false,
	}

	geo.CheckOctets("112") // if we have a 3 octet IP, add the last octet to make it routable

	// if Local, no need to check anything else
	if geo.isLocal() {
		LogGeo(&geo)
		return geo
	}

	// if Non-routable, no need to check anything else
	if geo.isNonRoutable() {
		LogGeo(&geo)
		return geo
	}

	// if we haven't set a redis address, we can't check the cache
	if redis_addr != "" {
		// using Redis - check there first
		hit := geo.checkRedisCache(redisClient, ip)
		if hit && (geo.CountryCode != "--") {
			geo.CacheHit = true
			LogGeo(&geo)
			return geo
		}
	}

	//if we get here, it's not found in the cache
	//ip should be routable, so call the location service
	geo.obtainGeoDat()

	geo.add2RedisCache(redisClient, ttl)
	LogGeo(&geo)
	return geo
}

func (g *GeoIPData) isLocal() bool {
	// let's "route" our local LAN
	if strings.HasPrefix(g.IP, "192.168.106.") {
		g.Located = true
		g.Routable = false
		g.ISP = "LaughingJ"
		g.CountryCode = "US"
		g.City = "Lewisville"
		g.CountryName = "United States"
		g.CacheHit = false
		LogGeo(g)
		return true
	}
	return false
}

func (g *GeoIPData) isNonRoutable() bool {
	nonRoutable := []string{
		"192.168.",
		"10.",
		"172.16.",
		"172.17.",
		"172.18.",
		"172.19.",
		"172.20.",
		"172.21.",
		"172.22.",
		"172.23.",
		"172.24.",
		"172.25.",
		"172.26.",
		"172.27.",
		"172.28.",
		"172.29.",
		"172.30.",
		"172.31.",
	}

	for _, v := range nonRoutable {
		if strings.HasPrefix(g.IP, v) {
			g.Routable = false
			g.Located = false
			g.Success = false
			g.CacheHit = false
			g.Error = fmt.Sprintf("Invalid public IPv4 or IPv6 address %s", g.IP)
			return true
		}
	}

	g.Routable = true
	return false
}

func (g *GeoIPData) obtainGeoDat() string {

	url := fmt.Sprintf("https://json.geoiplookup.io/%s", g.IP)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Accept-Encoding", "gzip")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		defer resp.Body.Close()
	}

	if resp.Status != "200 OK" {
		g.Error = fmt.Sprintf("GetGeoData received invalid response for IP: %s - %s", g.IP, resp.Status)
	}

	var reader io.ReadCloser
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		reader, _ = gzip.NewReader(resp.Body)
	default:
		reader = resp.Body
	}
	defer reader.Close()

	byt, err := io.ReadAll(reader)
	if err != nil {
		g.Error = fmt.Sprintf("Reading our reader failed - %s", err)
	}
	json.Unmarshal([]byte(byt), g)
	g.Located = true

	rlog.Debug(fmt.Sprintf("parsed Geo answer for IP:%s --> %v ", g.IP, g))
	jsonResult, _ := json.Marshal(g)
	return string(jsonResult)
}
