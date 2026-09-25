package sfu

import (
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"

	"thirdcoast.systems/rewind/internal/turn"
)

func TestPrepareICEServersPreservesDefaultSFUConfiguration(t *testing.T) {
	servers := []turn.Server{
		{URLs: []string{"stun:stun.example:3478"}},
		{URLs: []string{"turn:turn.example:3478?transport=udp"}, Username: "u", Credential: "c"},
	}
	got, policy, err := prepareICEServers(servers, false)
	if err != nil {
		t.Fatal(err)
	}
	if policy != webrtc.ICETransportPolicyAll || len(got) != len(servers) || len(got[1].URLs) != 1 {
		t.Fatalf("default ICE configuration = %#v, policy=%v", got, policy)
	}
}

func TestPrepareICEServersTLSOnlyUsesRelay443(t *testing.T) {
	got, policy, err := prepareICEServers([]turn.Server{
		{URLs: []string{"stun:stun.example:3478"}},
		{URLs: []string{
			"turn:turn.example:3478?transport=udp",
			"turns:turn.example:443?transport=tcp",
		}, Username: "u", Credential: "c"},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if policy != webrtc.ICETransportPolicyRelay || len(got) != 1 || len(got[0].URLs) != 1 || got[0].URLs[0] != "turns:turn.example:443?transport=tcp" {
		t.Fatalf("TLS-only ICE configuration = %#v, policy=%v", got, policy)
	}
}

func TestPrepareICEServersTLSOnlyFailsClosedWithoutRelay443(t *testing.T) {
	_, _, err := prepareICEServers([]turn.Server{{URLs: []string{"stun:stun.example:3478"}}}, true)
	if err == nil || !strings.Contains(err.Error(), "TLS-only") {
		t.Fatalf("TLS-only missing relay error = %v", err)
	}
}
