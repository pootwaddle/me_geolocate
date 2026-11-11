package me_geolocate

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestUnmarshalGeoShadow(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantWarn bool
	}{
		{"same", `{"ip":"1.2.3.4","isp":"Acme","country_code":"US","success":true}`, false},
		{"key case differs", `{"ip":"1.2.3.4","ISP":"Acme"}`, true},
		{"duplicate key", `{"ip":"1.2.3.4","isp":"A","isp":"B"}`, true},
		{"invalid utf-8", "{\"ip\":\"1.2.3.4\",\"isp\":\"Acme\xff\"}", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, nil))
			geo := GeoIPData{IP: "1.2.3.4"}
			if err := unmarshalGeo([]byte(c.body), &geo, logger, "test"); err != nil {
				t.Fatalf("v1 must stay the source of truth, got %v", err)
			}
			if got := strings.Contains(buf.String(), "json v2 shadow mismatch"); got != c.wantWarn {
				t.Errorf("warn=%v, want %v; log: %s", got, c.wantWarn, buf.String())
			}
		})
	}
}
