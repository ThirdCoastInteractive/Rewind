package sfu

import (
	"fmt"

	"github.com/pion/webrtc/v4"

	"thirdcoast.systems/rewind/internal/turn"
)

// prepareICEServers builds the SFU-only ICE configuration. The normal path
// preserves the configured STUN/TURN list and Pion's default all-candidates
// policy. TLS-only mode is deliberately stricter: it removes STUN and all
// non-TLS/non-443 TURN URLs, then requires relay candidates.
func prepareICEServers(servers []turn.Server, tlsOnly bool) ([]webrtc.ICEServer, webrtc.ICETransportPolicy, error) {
	if tlsOnly {
		filtered, err := turn.FilterTLS443Servers(servers)
		if err != nil {
			return nil, 0, fmt.Errorf("SFU TURN TLS-only configuration: %w", err)
		}
		return toPionICEServers(filtered), webrtc.ICETransportPolicyRelay, nil
	}
	return toPionICEServers(servers), webrtc.ICETransportPolicyAll, nil
}
