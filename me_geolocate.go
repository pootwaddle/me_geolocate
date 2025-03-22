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
	Block       bool
	CacheHit    string
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
	rlog.Printf("%+v\n", pong)
}

func (g *GeoIPData) checkRedisCache(redisClient *redis.Client, ip string) bool {
	var ctx = context.Background()

	jsonResult, err := redisClient.Get(ctx, ip).Result()
	if err == redis.Nil {
		g.Located = false
		g.CacheHit = Red + "false" + Reset
		return false
	}
	if err != nil {
		g.Located = false
		g.CacheHit = Red + "false" + Reset
		return false
	}

	json.Unmarshal([]byte(jsonResult), g)
	g.Located = true
	g.CacheHit = Green + "true" + Reset
	return true
}

func (g *GeoIPData) add2RedisCache(redisClient *redis.Client, minutes int) {
	ttl := time.Duration(time.Minute * time.Duration(minutes))
	ctx := context.Background()
	g.CacheHit = Red + "false" + Reset
	jsonResult, _ := json.Marshal(g)
	// we can call set with a `Key` and a `Value`.
	err := redisClient.Set(ctx, g.IP, jsonResult, ttl).Err()
	// if there has been an error setting the value
	// handle the error
	if err != nil {
		rlog.Errorf("Error adding to Redis Cache - %s", err)
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
		CacheHit:    BrightMagenta + "non-routable" + Reset,
	}

	geo.CheckOctets("112") // if we have a 3 octet IP, add the last octet to make it routable

	// if Local, no need to check anything else
	if geo.isLocal() {
		rlog.Printf("%+v", geo)
		return geo
	}

	// if Non-routable, no need to check anything else
	if geo.isNonRoutable() {
		rlog.Printf("%+v", geo)
		return geo
	}

	// if we haven't set a redis address, we can't check the cache
	if redis_addr != "" {
		// using Redis - check there first
		hit := geo.checkRedisCache(redisClient, ip)
		if hit {
			geo.CacheHit = Green + "true" + Reset
			rlog.Printf("%+v", geo)
			return geo
		}
	}

	//if we get here, it's not found in the cache
	//ip should be routable, so call the location service
	geo.obtainGeoDat()

	geo.add2RedisCache(redisClient, ttl)
	rlog.Printf("%+v\n", geo)
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
		g.CacheHit = Blue + "LaughingJ" + Reset
		rlog.Infof("%s is LaughingJ", g.IP)
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
			g.CacheHit = BrightMagenta + "non-routable" + Reset
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
