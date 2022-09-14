// Package geolocate handles the lookup of geo IP data from https://geoiplookup.io/api
// It looks first in the Redis cache
// And finally, on a miss to cache, it makes a call to
// https://json.geoiplookup.io/8.8.8.8 for the data and adds the data to the cache for next time
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

// https://json.geoiplookup.io/8.8.8.8
// seems up-to-date.   Limit 500 lookups per hour
type GeoIPData struct {
	IP             string  `json:"ip"`
	ISP            string  `json:"isp"`
	Org            string  `json:"org"`
	Hostname       string  `json:"hostname"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	PostalCode     string  `json:"postal_code"`
	City           string  `json:"city"`
	CountryCode    string  `json:"country_code"`
	CountryName    string  `json:"country_name"`
	ContinentCode  string  `json:"continent_code"`
	ContinentName  string  `json:"continent_name"`
	Region         string  `json:"region"`
	District       string  `json:"district"`
	TimezoneName   string  `json:"timezone_name"`
	ConnectionType string  `json:"connection_type"`
	AsnNumber      int     `json:"asn_number"`
	AsnOrg         string  `json:"asn_org"`
	Asn            string  `json:"asn"`
	CurrencyCode   string  `json:"currency_code"`
	CurrencyName   string  `json:"currency_name"`
	Success        bool    `json:"success"`
	Error          string  `json:"error"`
	Premium        bool    `json:"premium"`
	//my fields
	Located  bool `json:"located"`
	Routable bool `json:"routable"`
	Block    bool
	CacheHit bool
}

const ttl int = 129600 // 90 days in minutes  60*24*90
var redisClient *redis.Client
var redis_addr string

func init() {
	redis_addr = os.Getenv("REDIS_CONF")
	redisClient = redis.NewClient(&redis.Options{
		Addr:     redis_addr,
		Password: "",
		DB:       0,
	})
}

func (g *GeoIPData) checkRedisCache(redisClient *redis.Client, ip string) bool {
	var ctx = context.Background()

	jsonResult, err := redisClient.Get(ctx, ip).Result()
	if err == redis.Nil {
		g.Located = false
		return false
	}
	if err != nil {
		g.Located = false
		return false
	}

	json.Unmarshal([]byte(jsonResult), g)
	g.Located = true
	return true
}

func (g *GeoIPData) add2RedisCache(redisClient *redis.Client, minutes int) {
	ttl := time.Duration(time.Minute * time.Duration(minutes))
	ctx := context.Background()
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

// GetGeoData initializes a search for the geoLocation of an IP.  Module entry point
func GetGeoData(ip string) GeoIPData {
	geo := GeoIPData{
		IP:          ip,
		ISP:         "-----",
		CountryCode: "--",
		City:        "-----",
		CountryName: "-----",
		CacheHit:    false,
	}

	geo.CheckOctets("112")

	if redis_addr == "" {
		rlog.Error("Warning: REDIS_CONF not set")
		rlog.Printf("%+v\n", geo)
		return geo
	}

	// using Redis?  check there first
	geo.CacheHit = geo.checkRedisCache(redisClient, ip)
	if geo.CacheHit && geo.CountryCode != "--" {
		rlog.Printf("%+v\n", geo)
		return geo
	}

	// if we get here, it's not found in the cache, or hasn't been updated by the geo api
	// is it a routable IP?  if not, no need to call the service.
	// update GeoIPData, and add to cache
	if geo.isLocal() || !geo.isRoutable() {
		geo.add2RedisCache(redisClient, ttl)
		rlog.Printf("%+v\n", geo)
		return geo
	}

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
		g.Latitude = 33.000000
		g.Longitude = -97.000000
		g.PostalCode = "75067"
		g.ContinentCode = "NA"
		g.ContinentName = "North America"
		g.Region = "Texas"
		rlog.Infof("%s is LaughingJ", g.IP)
		return true
	}
	return false
}

func (g *GeoIPData) isRoutable() bool {
	// 192.168.0.0 to 192.168.255.255
	// 10.0.0.0 to 10.255.255.255
	// 172.16.0.0 to 172.31.255.255
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

	g.Routable = true

	for _, v := range nonRoutable {
		if strings.HasPrefix(g.IP, v) {
			g.Routable = false
			g.Success = false
			g.Error = fmt.Sprintf("Invalid public IPv4 or IPv6 address %s", g.IP)
		}
	}
	return true
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
