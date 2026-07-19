package main

import (
	"testing"
	"time"
)

func TestParseDurationExtended(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{in: "24h", want: 24 * time.Hour},
		{in: "90m", want: 90 * time.Minute},
		{in: "7d", want: 7 * 24 * time.Hour},
		{in: "1.5d", want: 36 * time.Hour},
		{in: "2w", want: 14 * 24 * time.Hour},
		{in: "-1d", wantErr: true},
		{in: "d", wantErr: true},
		{in: "sevend", wantErr: true},
		{in: "", wantErr: true},
		{in: "7x", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseDurationExtended(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %v", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("parseDurationExtended(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
