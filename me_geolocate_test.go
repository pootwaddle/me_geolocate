package me_geolocate

import (
	"testing"

	"github.com/go-redis/redis/v8"
)

// TestHelloName calls greetings.Hello with a name, checking
// for a valid return value.
func TestGetGeoData(t *testing.T) {
	rc := redis.NewClient(&redis.Options{
		Addr:     "192.168.106.143:6379",
		Password: "",
		DB:       0,
	})
	lc := make(map[string]string)
	ttl := 10
	ip := "8.8.8.8"
	want := "Google LLC"
	cached := false

	geo := GetGeoData(rc, lc, ttl, ip)
	got := geo.ISP
	if want != got {
		t.Errorf("want: %s\ngot: %s\n", want, got)
	}
	if geo.CacheHit != cached {
		t.Errorf("cache hit want: %v\ngot: %v\n", cached, geo.CacheHit)
	}
	// now should be in the cache
	ip = "8.8.8.8"
	want = "Google LLC"
	cached = true

	geo = GetGeoData(rc, lc, ttl, ip)
	got = geo.ISP
	if want != got {
		t.Errorf("want: %s\ngot: %s\n", want, got)
	}
	if geo.CacheHit != cached {
		t.Errorf("cache hit want: %v\ngot: %v\n", cached, geo.CacheHit)
	}

	ip = "1.1.1.1"
	want = "Cloudflare, Inc."
	geo = GetGeoData(rc, lc, ttl, ip)
	got = geo.ISP
	if want != got {
		t.Errorf("want: %s\ngot: %s\n", want, got)
	}

	ip = "1.1.1.1"
	want = "Cloudflare, Inc."
	geo = GetGeoData(rc, lc, ttl, ip)
	got = geo.ISP
	if want != got {
		t.Errorf("want: %s\ngot: %s\n", want, got)
	}

	ip = "192.168.1.1"
	want = "-----"
	want2 := "Invalid public IPv4 or IPv6 address"
	geo = GetGeoData(rc, lc, ttl, ip)
	got = geo.ISP
	got2 := geo.Error
	if want != got {
		t.Errorf("want: %s\ngot: %s\n", want, got)
	}
	if want2 != got2 {
		t.Errorf("want: %s\ngot: %s\n", want2, got2)
	}

	ip = "192.168.106.99"
	want = "LaughingJ"
	geo = GetGeoData(rc, lc, ttl, ip)
	got = geo.ISP
	if want != got {
		t.Errorf("want: %s\ngot: %s\n", want, got)
	}

	//localcache?
	ip = "1.1.1.1"
	want = "Cloudflare, Inc."
	geo = GetGeoData(nil, lc, ttl, ip)
	got = geo.ISP
	if want != got {
		t.Errorf("want: %s\ngot: %s\n", want, got)
	}
	if len(lc) != 1 {
		t.Error("map was not updated")
	}
	//localcache? - should be in there
	ip = "1.1.1.1"
	want = "Cloudflare, Inc."
	geo = GetGeoData(nil, lc, ttl, ip)
	got = geo.ISP
	if want != got {
		t.Errorf("want: %s\ngot: %s\n", want, got)
	}
	if len(lc) != 1 {
		t.Error("map was not updated")
	}

}
