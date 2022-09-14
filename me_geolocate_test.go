package me_geolocate

import (
	"os"
	"testing"
)

// TestHelloName calls greetings.Hello with a name, checking
// for a valid return value.
func TestGetGeoData(t *testing.T) {

	redis_addr := os.Getenv("REDIS_CONF")
	if redis_addr == "" {
		redis_addr = "127.0.0.1:6379"
	}

	ip := "8.8.8.8"
	want := "Google LLC"
	cached := false

	geo := GetGeoData(ip)
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

	geo = GetGeoData(ip)
	got = geo.ISP
	if want != got {
		t.Errorf("want: %s\ngot: %s\n", want, got)
	}
	if geo.CacheHit != cached {
		t.Errorf("cache hit want: %v\ngot: %v\n", cached, geo.CacheHit)
	}

	ip = "1.1.1.1"
	want = "Cloudflare, Inc."
	geo = GetGeoData(ip)
	got = geo.ISP
	if want != got {
		t.Errorf("want: %s\ngot: %s\n", want, got)
	}

	ip = "1.1.1.1"
	want = "Cloudflare, Inc."
	geo = GetGeoData(ip)
	got = geo.ISP
	if want != got {
		t.Errorf("want: %s\ngot: %s\n", want, got)
	}

	ip = "192.168.1.1"
	want = "-----"
	want2 := "Invalid public IPv4 or IPv6 address"
	geo = GetGeoData(ip)
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
	geo = GetGeoData(ip)
	got = geo.ISP
	if want != got {
		t.Errorf("want: %s\ngot: %s\n", want, got)
	}

}
