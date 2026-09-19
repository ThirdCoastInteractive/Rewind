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
	pc       *webrtc.PeerConnection
	ws       *threadSafeWriter
	userID   string
	username string
	kind     string // "camera" (default) | "screen"; screen peers are publish-only
}

// trackOwner records which publisher a forwarded track belongs to, so the SFU
// can relay a stable stream→host mapping to subscribers.
type trackOwner struct {
	streamID string
	userID   string
	username string
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
	api      *webrtc.API
	ice      []webrtc.ICEServer
	director *directorManager

	mu    sync.Mutex
	rooms map[string]*room

	upgrader websocket.Upgrader
}

// NewServer builds the Pion API (default codecs + interceptors) and ICE config.
func NewServer(dbc *db.DatabaseConnection, cfg *config.Config) (*Server, error) {
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

	return &Server{
		api:      api,
		ice:      buildICEServers(cfg),
		director: &directorManager{dbc: dbc},
		rooms:    make(map[string]*room),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true }, // behind the web proxy
		},
	}, nil
}

// buildICEServers parses comma-separated STUN/TURN URLs from config.
func buildICEServers(cfg *config.Config) []webrtc.ICEServer {
	var servers []webrtc.ICEServer
	if urls := splitURLs(cfg.STUNUrls); len(urls) > 0 {
		servers = append(servers, webrtc.ICEServer{URLs: urls})
	}
	if urls := splitURLs(cfg.TURNUrls); len(urls) > 0 {
		servers = append(servers, webrtc.ICEServer{
			URLs:       urls,
			Username:   cfg.TURNUsername,
			Credential: cfg.TURNPassword,
		})
	}
	return servers
}

func splitURLs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
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
func (s *Server) addTrack(r *room, t *webrtc.TrackRemote, userID, username, kind string) *webrtc.TrackLocalStaticRTP {
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
	r.trackOwners[t.ID()] = trackOwner{streamID: t.StreamID(), userID: userID, username: username}
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

// signalPeerConnections syncs every peer's senders with the room's track set and
// renegotiates as needed. Closed peers are pruned. Bounded retries avoid spinning.
func (s *Server) signalPeerConnections(r *room) {
	r.listLock.Lock()
	defer func() {
		r.listLock.Unlock()
		s.dispatchKeyFrame(r)
		s.broadcastIdentities(r)
	}()

	attemptSync := func() (tryAgain bool) {
		for i := range r.peers {
			if r.peers[i].pc.ConnectionState() == webrtc.PeerConnectionStateClosed {
				r.peers = append(r.peers[:i], r.peers[i+1:]...)
				return true
			}

			existing := map[string]bool{}
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
					if _, err := r.peers[i].pc.AddTrack(r.trackLocals[id]); err != nil {
						return true
					}
				}
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

	conn, err := s.upgrader.Upgrade(w, req, nil)
	if err != nil {
		slog.Error("sfu: ws upgrade", "error", err)
		return
	}
	ws := &threadSafeWriter{Conn: conn}
	defer ws.Close()

	pc, err := s.api.NewPeerConnection(webrtc.Configuration{ICEServers: s.ice})
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
	r.peers = append(r.peers, peerConnectionState{pc: pc, ws: ws, userID: userID, username: username, kind: kind})
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
		switch p {
		case webrtc.PeerConnectionStateFailed:
			_ = pc.Close()
		case webrtc.PeerConnectionStateClosed:
			s.signalPeerConnections(r)
		default:
		}
	})

	pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		trackLocal := s.addTrack(r, t, userID, username, kind)
		if trackLocal == nil {
			return
		}
		defer s.removeTrack(r, trackLocal)

		buf := make([]byte, 1500)
		for {
			n, _, err := t.Read(buf)
			if err != nil {
				return
			}
			if _, err = trackLocal.Write(buf[:n]); err != nil {
				return
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
			_ = pc.SetRemoteDescription(answer)
		}
	}
}
