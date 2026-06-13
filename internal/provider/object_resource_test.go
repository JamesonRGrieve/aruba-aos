// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import "testing"

func TestSubsetMatches(t *testing.T) {
	cases := []struct {
		name        string
		prior, cfg  string
		wantMatched bool
	}{
		{
			name:        "config subset of full device object — match (0-diff)",
			prior:       `{"vlan_id":40,"name":"IOT","uri":"/vlans/40","type":"VT_STATIC","is_voice_enabled":false}`,
			cfg:         `{"vlan_id":40,"name":"IOT"}`,
			wantMatched: true,
		},
		{
			name:        "declared key drifted — no match (update)",
			prior:       `{"vlan_id":40,"name":"IOT-OLD","uri":"/vlans/40"}`,
			cfg:         `{"vlan_id":40,"name":"IOT"}`,
			wantMatched: false,
		},
		{
			name:        "declared key missing on device — no match",
			prior:       `{"vlan_id":40,"uri":"/vlans/40"}`,
			cfg:         `{"vlan_id":40,"name":"IOT"}`,
			wantMatched: false,
		},
		{
			name:        "key order / whitespace insensitive — match",
			prior:       `{"name":"IOT","vlan_id":40}`,
			cfg:         "{\n  \"vlan_id\": 40,\n  \"name\": \"IOT\"\n}",
			wantMatched: true,
		},
		{
			name:        "nested object value compared structurally — match",
			prior:       `{"default_gateway":{"version":"IAV_IP_V4","octets":"192.168.2.1"},"name":"sw"}`,
			cfg:         `{"default_gateway":{"octets":"192.168.2.1","version":"IAV_IP_V4"}}`,
			wantMatched: true,
		},
		{
			name:        "nested object value drift — no match",
			prior:       `{"default_gateway":{"version":"IAV_IP_V4","octets":"192.168.2.1"}}`,
			cfg:         `{"default_gateway":{"version":"IAV_IP_V4","octets":"192.168.2.254"}}`,
			wantMatched: false,
		},
		{
			name:        "list value compared in order — match",
			prior:       `{"tagged":[40,50,58,82],"id":"Trk1"}`,
			cfg:         `{"tagged":[40,50,58,82]}`,
			wantMatched: true,
		},
		{
			name:        "invalid prior JSON — no match (fall back to diff)",
			prior:       `not json`,
			cfg:         `{"a":1}`,
			wantMatched: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := subsetMatches(tc.prior, tc.cfg); got != tc.wantMatched {
				t.Fatalf("subsetMatches() = %v, want %v", got, tc.wantMatched)
			}
		})
	}
}

func TestNormPath(t *testing.T) {
	for in, want := range map[string]string{
		"vlans/40":  "/vlans/40",
		"/vlans/40": "/vlans/40",
		" system ":  "/system",
		"/system":   "/system",
	} {
		if got := normPath(in); got != want {
			t.Errorf("normPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParentCollection(t *testing.T) {
	for in, want := range map[string]string{
		"/vlans-ports/58-41":               "/vlans-ports",
		"/vlans/58":                        "/vlans",
		"/vlans/81/ipaddresses/IAAM-1.2.3": "/vlans/81/ipaddresses",
		"/stp":                             "",
		"/system":                          "",
	} {
		if got := parentCollection(in); got != want {
			t.Errorf("parentCollection(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompactJSON(t *testing.T) {
	out, err := compactJSON([]byte("{\n \"b\": 2,\n \"a\": 1\n}"))
	if err != nil {
		t.Fatal(err)
	}
	// json.Marshal of a map sorts keys; whitespace is removed.
	if out != `{"a":1,"b":2}` {
		t.Fatalf("compactJSON = %q", out)
	}
}
