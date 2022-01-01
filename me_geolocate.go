// Package geolocate handles the lookup of geo IP data from https://geoiplookup.io/api
// It looks first in the Redis cache
// If no Redis configuration it looks in program-defined map
// And finally, on a miss to either of the caches, it makes a call to
// https://json.geoiplookup.io/8.8.8.8 for the data and adds the data to the appropriate cache for next time
package me_geolocate

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
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

func (g *GeoIPData) checkLocalCache(localCache map[string]string, ip string) bool {
	var s sync.Mutex
	s.Lock()
	jsonResult, found := localCache[ip]
	s.Unlock()

	if found {
		json.Unmarshal([]byte(jsonResult), g)
	}
	g.Located = true
	return found
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

func (g *GeoIPData) add2LocalCache(localCache map[string]string) {
	jsonResult, _ := json.Marshal(g)
	var s sync.Mutex
	s.Lock()
	localCache[g.IP] = string(jsonResult)
	s.Unlock()

}

// GetGeoData initializes a search for the geoLocation of an IP.  We are passed a pointer to a redis Client or <nil>
// a localCache (map structure) the IP we want and finally the TTL for how long to keep the IP in the Redis cache (in minutes)
func GetGeoData(redisClient *redis.Client, localCache map[string]string, ttl int, ip string) GeoIPData {
	geo := GeoIPData{
		IP:          ip,
		ISP:         "-----",
		CountryCode: "--",
		City:        "-----",
		CountryName: "-----",
	}

	var found bool

	// using Redis?  check there first
	if redisClient != nil {
		found = geo.checkRedisCache(redisClient, ip)
		geo.CacheHit = found
	} else {
		// in localCache?
		found = geo.checkLocalCache(localCache, ip)
		geo.CacheHit = found
	}

	// found in one of our caches? skeedaddle
	if found {
		return geo
	}

	// if we get here, it's not found in a cache
	// is it a routable IP?  if not, no need to call the service.
	// update GeoIPData, and add to cache
	if geo.isLocal() || !geo.isRoutable() {
		if redisClient != nil {
			geo.add2RedisCache(redisClient, ttl)
		} else {
			geo.add2LocalCache(localCache)
		}
		return geo
	}

	//finally, call the location service
	geo.obtainGeoDat()
	if redisClient != nil {
		geo.add2RedisCache(redisClient, ttl)
	} else {
		geo.add2LocalCache(localCache)
	}
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

	for _, v := range nonRoutable {
		if strings.HasPrefix(g.IP, v) {
			g.Routable = false
			g.Success = false
			g.Error = fmt.Sprintf("Invalid public IPv4 or IPv6 address %s", g.IP)
		}
	}
	g.Routable = true
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

	byt, err := ioutil.ReadAll(reader)
	if err != nil {
		g.Error = fmt.Sprintf("Reading our reader failed - %s", err)
	}
	json.Unmarshal([]byte(byt), g)
	g.Located = true

	rlog.Debug(fmt.Sprintf("parsed Geo answer for IP:%s --> %v ", g.IP, g))
	jsonResult, _ := json.Marshal(g)
	return string(jsonResult)
}
