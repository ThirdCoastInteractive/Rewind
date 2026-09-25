// Package sfu implements a minimal Pion-based Selective Forwarding Unit for
// Rewind's producer sessions. Each show note is a "room"; peers (hosts and
// viewers) connect over WebSocket, publish their camera/mic tracks, and receive
// every other peer's tracks forwarded by the SFU. Signaling (offer/answer/ICE)
// rides the same WebSocket. UI state stays on the SSE hubs in the web service;
// this service only moves media.
//
// The forwarding/renegotiation approach follows the canonical Pion sfu-ws
// example, generalized to multiple rooms keyed by show-note id.
package sfu

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"

	"thirdcoast.systems/rewind/internal/config"
	"thirdcoast.systems/rewind/internal/db"
	"thirdcoast.systems/rewind/internal/turn"
)

// threadSafeWriter serializes concurrent writes to a single WebSocket.
type threadSafeWriter struct {
	*websocket.Conn
	sync.Mutex
}

func (t *threadSafeWriter) WriteJSON(v any) error {
	t.Lock()
	defer t.Unlock()
	return t.Conn.WriteJSON(v)
}

// websocketMessage is the signaling envelope exchanged with each peer.
type websocketMessage struct {
	Event string `json:"event"` // "offer" | "answer" | "candidate"
	Data  string `json:"data"`
}

type peerConnectionState struct {
	pc         *webrtc.PeerConnection
	ws         *threadSafeWriter
	userID     string
	username   string
	kind       string // "camera" (default) | "screen"; screen peers are publish-only
	needsSync  bool
	forceOffer bool
}

// trackOwner records which publisher a forwarded track belongs to, so the SFU
// can relay a stable stream→host mapping to subscribers.
type trackOwner struct {
	streamID   string
	userID     string
	username   string
	source     *webrtc.PeerConnection
	sourceSSRC uint32
}

// room holds the peers and forwarded tracks for one show note.
type room struct {
	listLock    sync.Mutex
	peers       []peerConnectionState
	trackLocals map[string]*webrtc.TrackLocalStaticRTP
	trackOwners map[string]trackOwner // track ID -> publisher identity
	streamKinds map[string]string     // stream ID -> "camera" | "screen"
}

// identityMsg is one publisher's stable identity, relayed to subscribers so
// scene sources can bind to a specific host's camera or screen (stable slotting +
// name labels).
type identityMsg struct {
	StreamID string `json:"streamId"`
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Kind     string `json:"kind"` // "camera" | "screen"
}

// Server is the SFU: an HTTP handler plus the room registry and Pion API.
type Server struct {
	api              *webrtc.API
	turn             *turn.Provider
	turnTLSOnly      bool
	director         *directorManager
	keyFrameDispatch func(*room)

	mu    sync.Mutex
	rooms map[string]*room

	upgrader websocket.Upgrader
}

// NewServer builds the Pion API (default codecs + interceptors) and lazy ICE provider.
func NewServer(dbc *db.DatabaseConnection, cfg *config.Config, providers ...*turn.Provider) (*Server, error) {
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		return nil, err
	}

	// Pin ICE to a fixed UDP port range so it matches the container's published
	// ports (docker-compose exposes 50000-50100/udp). Optionally advertise a
	// 1:1 NAT IP (the host's reachable address) for remote/Docker setups.
	se := webrtc.SettingEngine{}
	se.SetEphemeralUDPPortRange(50000, 50100)
	if natIP := strings.TrimSpace(os.Getenv("SFU_NAT_IP")); natIP != "" {
		if err := se.SetICEAddressRewriteRules(webrtc.ICEAddressRewriteRule{
			External:        []string{natIP},
			AsCandidateType: webrtc.ICECandidateTypeHost,
			Mode:            webrtc.ICEAddressRewriteReplace,
		}); err != nil {
			return nil, err
		}
	}

	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(m),
		webrtc.WithInterceptorRegistry(i),
		webrtc.WithSettingEngine(se),
	)

	iceProvider, err := selectTURNProvider(cfg, providers...)
	if err != nil {
		return nil, err
	}
	return &Server{
		api:         api,
		turn:        iceProvider,
		turnTLSOnly: cfg != nil && cfg.SFUTurnTLSOnly,
		director:    &directorManager{dbc: dbc},
		rooms:       make(map[string]*room),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true }, // behind the web proxy
		},
	}, nil
}

func selectTURNProvider(cfg *config.Config, providers ...*turn.Provider) (*turn.Provider, error) {
	if len(providers) > 0 && providers[0] != nil {
		return providers[0], nil
	}
	return turn.NewProvider(turn.Config{
		STUNURLs:     cfg.STUNUrls,
		TURNURLs:     cfg.TURNUrls,
		TURNUsername: cfg.TURNUsername,
		TURNPassword: cfg.TURNPassword,
		TurnKeyID:    cfg.CFTurnKeyID,
		TurnAPIToken: cfg.CFTurnAPIToken,
	})
}

// Handler returns the SFU HTTP mux (signaling WebSocket + health check).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/signal", s.handleSignal)
	return mux
}

func (s *Server) getRoom(id string) *room {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rooms[id]
	if !ok {
		r = &room{
			trackLocals: make(map[string]*webrtc.TrackLocalStaticRTP),
			trackOwners: make(map[string]trackOwner),
			streamKinds: make(map[string]string),
		}
		s.rooms[id] = r
	}
	return r
}

// addTrack creates a forwarding track for an inbound remote track, records its
// publisher identity, and re-signals.
func (s *Server) addTrack(r *room, t *webrtc.TrackRemote, source *webrtc.PeerConnection, userID, username, kind string) *webrtc.TrackLocalStaticRTP {
	r.listLock.Lock()
	defer func() {
		r.listLock.Unlock()
		s.signalPeerConnections(r)
	}()
	trackLocal, err := webrtc.NewTrackLocalStaticRTP(t.Codec().RTPCodecCapability, t.ID(), t.StreamID())
	if err != nil {
		slog.Error("sfu: new local track", "error", err)
		return nil
	}
	r.trackLocals[t.ID()] = trackLocal
	r.trackOwners[t.ID()] = trackOwner{
		streamID:   t.StreamID(),
		userID:     userID,
		username:   username,
		source:     source,
		sourceSSRC: uint32(t.SSRC()),
	}
	if kind == "" {
		kind = "camera"
	}
	r.streamKinds[t.StreamID()] = kind
	return trackLocal
}

// removeTrack drops a forwarding track and re-signals.
func (s *Server) removeTrack(r *room, t *webrtc.TrackLocalStaticRTP) {
	r.listLock.Lock()
	defer func() {
		r.listLock.Unlock()
		s.signalPeerConnections(r)
	}()
	delete(r.trackLocals, t.ID())
	delete(r.trackOwners, t.ID())
}

// signalPeerConnections marks every peer dirty, syncs senders, and renegotiates
// stable peers. A peer with an outstanding local offer stays dirty until its
// answer arrives; this prevents overlapping offers while preserving updates.
func (s *Server) signalPeerConnections(r *room) {
	r.listLock.Lock()
	for i := range r.peers {
		r.peers[i].needsSync = true
	}
	s.syncPeerConnectionsLocked(r)
	r.listLock.Unlock()
	s.dispatchKeyFrame(r)
	s.broadcastIdentities(r)
}

// drainPeerConnections handles a successful answer without marking clean peers
// dirty. This is the only path that drains work accumulated during an offer.
func (s *Server) drainPeerConnections(r *room) {
	r.listLock.Lock()
	s.syncPeerConnectionsLocked(r)
	r.listLock.Unlock()
	// The subscriber's new sender binding does not exist when the initial offer
	// is signaled. Request a fresh source keyframe after its answer is applied.
	s.dispatchKeyFrame(r)
}

func (s *Server) syncPeerConnectionsLocked(r *room) {
	attemptSync := func() (tryAgain bool) {
		for i := 0; i < len(r.peers); i++ {
			if r.peers[i].pc.ConnectionState() == webrtc.PeerConnectionStateClosed {
				r.peers = append(r.peers[:i], r.peers[i+1:]...)
				i--
				return true
			}
			if !r.peers[i].needsSync || !peerReadyForOffer(&r.peers[i]) {
				continue
			}

			existing := map[string]bool{}
			changed := false
			for _, sender := range r.peers[i].pc.GetSenders() {
				if sender.Track() == nil {
					continue
				}
				existing[sender.Track().ID()] = true
				// Drop senders whose source track is gone.
				if _, ok := r.trackLocals[sender.Track().ID()]; !ok {
					if err := r.peers[i].pc.RemoveTrack(sender); err != nil {
						return true
					}
					changed = true
					r.peers[i].forceOffer = true
				}
			}
			// Never forward a peer its own published tracks.
			for _, receiver := range r.peers[i].pc.GetReceivers() {
				if receiver.Track() == nil {
					continue
				}
				existing[receiver.Track().ID()] = true
			}
			// Add any room tracks this peer isn't yet receiving. Screen
			// publishers are publish-only (they receive nothing), and a peer never
			// receives its own user's media back across their other connections
			// (e.g. a host's camera peer receiving that host's own screen).
			if r.peers[i].kind != "screen" {
				for id := range r.trackLocals {
					if existing[id] {
						continue
					}
					if owner, ok := r.trackOwners[id]; ok && owner.userID != "" && owner.userID == r.peers[i].userID {
						continue
					}
					sender, err := r.peers[i].pc.AddTrack(r.trackLocals[id])
					if err != nil {
						return true
					}
					changed = true
					r.peers[i].forceOffer = true
					if owner, ok := r.trackOwners[id]; ok && owner.source != nil {
						go drainSenderRTCP(sender, owner.source, owner.sourceSSRC)
					}
				}
			}
			if !changed && !r.peers[i].forceOffer {
				r.peers[i].needsSync = false
				continue
			}

			offer, err := r.peers[i].pc.CreateOffer(nil)
			if err != nil {
				return true
			}
			if err = r.peers[i].pc.SetLocalDescription(offer); err != nil {
				return true
			}
			b, err := json.Marshal(offer)
			if err != nil {
				return true
			}
			if err = r.peers[i].ws.WriteJSON(&websocketMessage{Event: "offer", Data: string(b)}); err != nil {
				return true
			}
			r.peers[i].needsSync = false
			r.peers[i].forceOffer = false
		}
		return false
	}

	for attempt := 0; ; attempt++ {
		if attempt == 25 {
			// Give up for now; try again shortly.
			go func() {
				time.Sleep(3 * time.Second)
				s.signalPeerConnections(r)
			}()
			return
		}
		if !attemptSync() {
			break
		}
	}
}

func peerReadyForOffer(peer *peerConnectionState) bool {
	return peer.needsSync && peer.pc.SignalingState() == webrtc.SignalingStateStable
}

// broadcastIdentities pushes the current stream→host mapping to every peer, so
// each client can bind webcam sources to a specific host and label them. A host
// publishes audio+video under one stream id, so the map is deduped by stream.
func (s *Server) broadcastIdentities(r *room) {
	r.listLock.Lock()
	seen := make(map[string]identityMsg, len(r.trackOwners))
	for _, o := range r.trackOwners {
		if o.streamID == "" {
			continue
		}
		kind := r.streamKinds[o.streamID]
		if kind == "" {
			kind = "camera"
		}
		seen[o.streamID] = identityMsg{StreamID: o.streamID, UserID: o.userID, Username: o.username, Kind: kind}
	}
	list := make([]identityMsg, 0, len(seen))
	for _, m := range seen {
		list = append(list, m)
	}
	writers := make([]*threadSafeWriter, 0, len(r.peers))
	for i := range r.peers {
		writers = append(writers, r.peers[i].ws)
	}
	r.listLock.Unlock()

	b, err := json.Marshal(list)
	if err != nil {
		return
	}
	for _, ws := range writers {
		_ = ws.WriteJSON(&websocketMessage{Event: "meta", Data: string(b)})
	}
}

// dispatchKeyFrame asks every sender's source for a fresh keyframe (PLI) so newly
// subscribed peers render immediately rather than waiting for the next one.
func (s *Server) dispatchKeyFrame(r *room) {
	if s.keyFrameDispatch != nil {
		s.keyFrameDispatch(r)
		return
	}
	r.listLock.Lock()
	defer r.listLock.Unlock()
	for i := range r.peers {
		for _, receiver := range r.peers[i].pc.GetReceivers() {
			if receiver.Track() == nil {
				continue
			}
			_ = r.peers[i].pc.WriteRTCP([]rtcp.Packet{
				&rtcp.PictureLossIndication{MediaSSRC: uint32(receiver.Track().SSRC())},
			})
		}
	}
}

// handleSignal upgrades the WebSocket and runs the peer lifecycle. The web service
// has already authenticated the caller and passes room/user/role as query params.
func (s *Server) handleSignal(w http.ResponseWriter, req *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("sfu: panic in signal handler", "panic", rec)
		}
	}()

	roomID := req.URL.Query().Get("room")
	userID := req.URL.Query().Get("user")
	username := req.URL.Query().Get("username")
	role := req.URL.Query().Get("role")
	kind := req.URL.Query().Get("kind")
	if kind == "" {
		kind = "camera"
	}
	if roomID == "" {
		http.Error(w, "missing room", http.StatusBadRequest)
		return
	}

	iceServers, err := s.turn.Servers(req.Context())
	if err != nil {
		slog.Error("sfu: TURN credentials unavailable", "error", err)
		http.Error(w, "ICE service unavailable", http.StatusServiceUnavailable)
		return
	}
	pionICEServers, icePolicy, err := prepareICEServers(iceServers, s.turnTLSOnly)
	if err != nil {
		slog.Error("sfu: ICE configuration unavailable", "error", err)
		http.Error(w, "ICE service unavailable", http.StatusServiceUnavailable)
		return
	}
	conn, err := s.upgrader.Upgrade(w, req, nil)
	if err != nil {
		slog.Error("sfu: ws upgrade", "error", err)
		return
	}
	ws := &threadSafeWriter{Conn: conn}
	defer ws.Close()

	pc, err := s.api.NewPeerConnection(webrtc.Configuration{
		ICEServers:         pionICEServers,
		ICETransportPolicy: icePolicy,
	})
	if err != nil {
		slog.Error("sfu: new peer connection", "error", err)
		return
	}
	defer pc.Close()

	// Recvonly transceivers so the SFU can ingest whatever the peer publishes.
	for _, typ := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
		if _, err := pc.AddTransceiverFromKind(typ, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		}); err != nil {
			slog.Error("sfu: add transceiver", "error", err)
			return
		}
	}

	r := s.getRoom(roomID)
	r.listLock.Lock()
	r.peers = append(r.peers, peerConnectionState{
		pc:         pc,
		ws:         ws,
		userID:     userID,
		username:   username,
		kind:       kind,
		needsSync:  true,
		forceOffer: true,
	})
	r.listLock.Unlock()

	pc.OnICECandidate(func(i *webrtc.ICECandidate) {
		if i == nil {
			return
		}
		b, err := json.Marshal(i.ToJSON())
		if err != nil {
			return
		}
		_ = ws.WriteJSON(&websocketMessage{Event: "candidate", Data: string(b)})
	})

	pc.OnConnectionStateChange(func(p webrtc.PeerConnectionState) {
		if p == webrtc.PeerConnectionStateConnected || p == webrtc.PeerConnectionStateFailed {
			logSelectedCandidate(pc, userID, p)
		}
		switch p {
		case webrtc.PeerConnectionStateFailed:
			_ = pc.Close()
		case webrtc.PeerConnectionStateClosed:
			s.signalPeerConnections(r)
		default:
		}
	})

	pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		trackLocal := s.addTrack(r, t, pc, userID, username, kind)
		if trackLocal == nil {
			return
		}
		defer s.removeTrack(r, trackLocal)

		buf := make([]byte, 1500)
		var lastWriteLog time.Time
		for {
			n, _, err := t.Read(buf)
			if err != nil {
				return
			}
			// TrackLocalStaticRTP fans out to every subscriber. A binding can
			// reject a packet while its peer is still negotiating or closing;
			// keep forwarding to the healthy bindings instead of removing the
			// room's shared track on that transient error.
			if err := writeForwardedRTP(trackLocal, buf[:n]); err != nil {
				now := time.Now()
				if lastWriteLog.IsZero() || now.Sub(lastWriteLog) >= 5*time.Second {
					slog.Warn("sfu: forwarded RTP binding rejected packet", "track", t.ID(), "user", userID, "error", err)
					lastWriteLog = now
				}
			}
		}
	})

	// Director presence (hosts only; viewers don't control playback).
	var roomUUID, userUUID pgtype.UUID
	trackDirector := role != "viewer" && roomUUID.Scan(roomID) == nil && userUUID.Scan(userID) == nil
	if trackDirector {
		s.director.onJoin(context.Background(), roomUUID, userUUID)
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					s.director.ping(context.Background(), roomUUID, userUUID)
				}
			}
		}()
		defer s.director.onLeave(context.Background(), roomUUID, userUUID)
	}

	s.signalPeerConnections(r)

	// Read loop: apply the peer's answers and trickled ICE candidates.
	message := &websocketMessage{}
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if err := json.Unmarshal(raw, &message); err != nil {
			continue
		}
		switch message.Event {
		case "candidate":
			var candidate webrtc.ICECandidateInit
			if err := json.Unmarshal([]byte(message.Data), &candidate); err != nil {
				continue
			}
			_ = pc.AddICECandidate(candidate)
		case "answer":
			var answer webrtc.SessionDescription
			if err := json.Unmarshal([]byte(message.Data), &answer); err != nil {
				continue
			}
			if err := pc.SetRemoteDescription(answer); err != nil {
				slog.Warn("sfu: set remote answer", "user", userID, "error", err)
				continue
			}
			s.drainPeerConnections(r)
		}
	}
}

// writeForwardedRTP deliberately keeps a packet source alive when one of the
// TrackLocal bindings rejects a packet. TrackLocalStaticRTP reports combined
// binding errors while still writing to the healthy bindings.
func writeForwardedRTP(track *webrtc.TrackLocalStaticRTP, packet []byte) error {
	_, err := track.Write(packet)
	return err
}

// drainSenderRTCP keeps Pion's sender interceptors active and relays decoder
// feedback to the publisher that owns the forwarded track. Without draining
// this reader, subscriber PLI/NACK packets remain queued at the SFU and a
// newly subscribed decoder can receive RTP without ever receiving a keyframe.
// NACK packets are intentionally consumed by Pion's sender interceptor rather
// than forwarded, because the SFU sender owns the subscriber-side SSRC.
func drainSenderRTCP(sender *webrtc.RTPSender, source *webrtc.PeerConnection, sourceSSRC uint32) {
	for {
		packets, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		feedback := forwardableRTCP(packets, sourceSSRC)
		if len(feedback) == 0 {
			continue
		}
		_ = source.WriteRTCP(feedback)
	}
}

func forwardableRTCP(packets []rtcp.Packet, sourceSSRC uint32) []rtcp.Packet {
	feedback := make([]rtcp.Packet, 0, len(packets))
	for _, packet := range packets {
		switch packet := packet.(type) {
		case *rtcp.PictureLossIndication:
			copy := *packet
			copy.MediaSSRC = sourceSSRC
			feedback = append(feedback, &copy)
		case *rtcp.FullIntraRequest:
			copy := *packet
			// FIR targets are carried in the FCI entries; RFC 5104 requires
			// the common MediaSSRC field to remain zero.
			copy.MediaSSRC = 0
			copy.FIR = append([]rtcp.FIREntry(nil), packet.FIR...)
			for i := range copy.FIR {
				copy.FIR[i].SSRC = sourceSSRC
			}
			feedback = append(feedback, &copy)
		}
	}
	return feedback
}

// logSelectedCandidate records only non-sensitive ICE metadata. In particular,
// it omits candidate URLs, addresses, credentials, and SDP so this remains
// useful in production logs without exposing TURN material.
func logSelectedCandidate(pc *webrtc.PeerConnection, userID string, state webrtc.PeerConnectionState) {
	attrs := []any{"user", userID, "state", state.String()}
	stats := pc.GetStats()
	for _, raw := range stats {
		transport, ok := raw.(webrtc.TransportStats)
		if !ok || transport.SelectedCandidatePairID == "" {
			continue
		}
		pair, ok := stats[transport.SelectedCandidatePairID].(webrtc.ICECandidatePairStats)
		if !ok {
			continue
		}
		local, localOK := stats[pair.LocalCandidateID].(webrtc.ICECandidateStats)
		remote, remoteOK := stats[pair.RemoteCandidateID].(webrtc.ICECandidateStats)
		if localOK {
			attrs = append(attrs, "local_candidate_type", local.CandidateType.String(), "local_protocol", local.Protocol, "local_relay_protocol", local.RelayProtocol)
		}
		if remoteOK {
			attrs = append(attrs, "remote_candidate_type", remote.CandidateType.String(), "remote_protocol", remote.Protocol)
		}
		attrs = append(attrs, "nominated", pair.Nominated)
		break
	}
	slog.Info("sfu: peer ICE state", attrs...)
}

func toPionICEServers(servers []turn.Server) []webrtc.ICEServer {
	out := make([]webrtc.ICEServer, 0, len(servers))
	for _, server := range servers {
		out = append(out, webrtc.ICEServer{
			URLs:       append([]string(nil), server.URLs...),
			Username:   server.Username,
			Credential: server.Credential,
		})
	}
	return out
}
