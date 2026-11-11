package me_geolocate

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"log/slog"
)

// unmarshalGeo decodes b into geo with encoding/json (v1), which stays the
// source of truth. It also decodes a copy with encoding/json/v2 and logs a
// warning when the two disagree, so v2's stricter defaults (case-sensitive
// keys, invalid UTF-8 and duplicate keys rejected) can be checked against
// real geoip and Redis data before switching. Remove once the trial ends.
func unmarshalGeo(b []byte, geo *GeoIPData, logger *slog.Logger, source string) error {
	before := *geo
	err := json.Unmarshal(b, geo)

	shadow := before
	errV2 := jsonv2.Unmarshal(b, &shadow)
	if (err == nil) != (errV2 == nil) || (err == nil && shadow != *geo) {
		raw := string(b)
		if len(raw) > 500 {
			raw = raw[:500] + "..."
		}
		logger.Warn("json v2 shadow mismatch",
			"source", source, "ip", before.IP,
			"v1_err", errString(err), "v2_err", errString(errV2),
			"v1", *geo, "v2", shadow, "raw", raw)
	}
	return err
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
