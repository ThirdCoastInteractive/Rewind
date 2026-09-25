package sfu

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"thirdcoast.systems/rewind/internal/config"
)

type fanoutWriter struct {
	fail   bool
	writes int
}

func (w *fanoutWriter) WriteRTP(_ *rtp.Header, payload []byte) (int, error) {
	if w.fail {
		return 0, errors.New("subscriber is closing")
	}
	w.writes++
	return len(payload), nil
}

func (w *fanoutWriter) Write(payload []byte) (int, error) {
	return w.WriteRTP(&rtp.Header{}, payload)
}

type fanoutContext struct {
	id     string
	codec  webrtc.RTPCodecCapability
	writer webrtc.TrackLocalWriter
}

func (c *fanoutContext) CodecParameters() []webrtc.RTPCodecParameters {
	return []webrtc.RTPCodecParameters{{RTPCodecCapability: c.codec, PayloadType: 96}}
}
func (c *fanoutContext) HeaderExtensions() []webrtc.RTPHeaderExtensionParameter { return nil }
func (c *fanoutContext) SSRC() webrtc.SSRC                                      { return 1 }
func (c *fanoutContext) SSRCRetransmission() webrtc.SSRC                        { return 0 }
func (c *fanoutContext) SSRCForwardErrorCorrection() webrtc.SSRC                { return 0 }
func (c *fanoutContext) WriteStream() webrtc.TrackLocalWriter                   { return c.writer }
func (c *fanoutContext) ID() string                                             { return c.id }
func (c *fanoutContext) RTCPReader() interceptor.RTCPReader                     { return nil }

func TestWriteForwardedRTPKeepsHealthyBindingAfterSubscriberError(t *testing.T) {
	codec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}
	track, err := webrtc.NewTrackLocalStaticRTP(codec, "camera", "stream")
	if err != nil {
		t.Fatal(err)
	}
	failed := &fanoutWriter{fail: true}
	healthy := &fanoutWriter{}
	for _, ctx := range []*fanoutContext{
		{id: "failed", codec: codec, writer: failed},
		{id: "healthy", codec: codec, writer: healthy},
	} {
		if _, err := track.Bind(ctx); err != nil {
			t.Fatal(err)
		}
	}

	packet, err := (&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 96}, Payload: []byte{1, 2, 3}}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	writeForwardedRTP(track, packet)
	if healthy.writes != 1 {
		t.Fatalf("healthy subscriber writes = %d, want 1", healthy.writes)
	}
}

func TestForwardableRTCPKeepsDecoderFeedback(t *testing.T) {
	pli := &rtcp.PictureLossIndication{MediaSSRC: 11}
	fir := &rtcp.FullIntraRequest{MediaSSRC: 11, FIR: []rtcp.FIREntry{{SSRC: 11}}}
	nack := &rtcp.TransportLayerNack{MediaSSRC: 11}
	packets := forwardableRTCP([]rtcp.Packet{
		pli,
		fir,
		nack,
		&rtcp.ReceiverReport{},
	}, 22)
	if len(packets) != 2 {
		t.Fatalf("forwardable RTCP packets = %d, want 2", len(packets))
	}
	if _, ok := packets[0].(*rtcp.PictureLossIndication); !ok {
		t.Fatalf("first feedback packet = %T", packets[0])
	}
	if packets[0].(*rtcp.PictureLossIndication).MediaSSRC != 22 {
		t.Fatalf("PLI media SSRC = %d, want 22", packets[0].(*rtcp.PictureLossIndication).MediaSSRC)
	}
	if _, ok := packets[1].(*rtcp.FullIntraRequest); !ok {
		t.Fatalf("second feedback packet = %T", packets[1])
	}
	if packets[1].(*rtcp.FullIntraRequest).FIR[0].SSRC != 22 {
		t.Fatalf("FIR media SSRC = %d, want 22", packets[1].(*rtcp.FullIntraRequest).FIR[0].SSRC)
	}
	if packets[1].(*rtcp.FullIntraRequest).MediaSSRC != 0 {
		t.Fatalf("FIR common media SSRC = %d, want 0", packets[1].(*rtcp.FullIntraRequest).MediaSSRC)
	}
	if pli.MediaSSRC != 11 || fir.MediaSSRC != 11 || fir.FIR[0].SSRC != 11 || nack.MediaSSRC != 11 {
		t.Fatal("forwardable RTCP mutated subscriber feedback")
	}
}

func TestSFUOfferGateDrainsTrackUpdateAfterAnswer(t *testing.T) {
	upgrade := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	serverConnCh := make(chan *websocket.Conn, 1)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrade.Upgrade(w, r, nil)
		if err == nil {
			serverConnCh <- conn
		}
	}))
	defer httpServer.Close()
	clientConn, _, err := websocket.DefaultDialer.Dial("ws"+httpServer.URL[len("http"):], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	serverConn := <-serverConnCh
	defer serverConn.Close()

	offers := make(chan websocketMessage, 4)
	go func() {
		for {
			_, data, readErr := clientConn.ReadMessage()
			if readErr != nil {
				return
			}
			var message websocketMessage
			if json.Unmarshal(data, &message) == nil && message.Event == "offer" {
				offers <- message
			}
		}
	}()

	srv, err := NewServer(nil, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	local, err := srv.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	remote, err := srv.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	if _, err := local.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		t.Fatal(err)
	}

	r := &room{
		trackLocals: make(map[string]*webrtc.TrackLocalStaticRTP),
		trackOwners: make(map[string]trackOwner),
		streamKinds: make(map[string]string),
		peers: []peerConnectionState{{
			pc:         local,
			ws:         &threadSafeWriter{Conn: serverConn},
			userID:     "viewer",
			needsSync:  true,
			forceOffer: true,
		}},
	}
	dispatches := 0
	srv.keyFrameDispatch = func(*room) { dispatches++ }
	r.listLock.Lock()
	srv.syncPeerConnectionsLocked(r)
	r.listLock.Unlock()
	firstOffer := waitForOffer(t, offers)

	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2,
	}, "audio", "publisher-stream")
	if err != nil {
		t.Fatal(err)
	}
	r.listLock.Lock()
	r.trackLocals[track.ID()] = track
	r.trackOwners[track.ID()] = trackOwner{userID: "publisher"}
	r.peers[0].needsSync = true
	// A track update during have-local-offer remains pending and cannot emit a
	// second offer.
	srv.syncPeerConnectionsLocked(r)
	r.listLock.Unlock()
	select {
	case <-offers:
		t.Fatal("SFU emitted overlapping offer before answer")
	case <-time.After(100 * time.Millisecond):
	}

	// The answer makes the peer stable; drain must add the pending track and
	// emit exactly one follow-up offer.
	answerOffer(t, local, remote, firstOffer)
	srv.drainPeerConnections(r)
	if dispatches != 1 {
		t.Fatalf("post-answer keyframe dispatches = %d, want 1", dispatches)
	}
	secondOffer := waitForOffer(t, offers)
	if secondOffer.Data == firstOffer.Data {
		t.Fatal("follow-up offer did not change after pending track update")
	}
	foundTrack := false
	for _, sender := range local.GetSenders() {
		if sender.Track() != nil && sender.Track().ID() == track.ID() {
			foundTrack = true
		}
	}
	if !foundTrack {
		t.Fatal("pending track was not added before follow-up offer")
	}
}

func waitForOffer(t *testing.T, offers <-chan websocketMessage) websocketMessage {
	t.Helper()
	select {
	case offer := <-offers:
		return offer
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SFU offer")
		return websocketMessage{}
	}
}

func answerOffer(t *testing.T, local, remote *webrtc.PeerConnection, message websocketMessage) {
	t.Helper()
	var offer webrtc.SessionDescription
	if err := json.Unmarshal([]byte(message.Data), &offer); err != nil {
		t.Fatal(err)
	}
	if err := remote.SetRemoteDescription(offer); err != nil {
		t.Fatal(err)
	}
	answer, err := remote.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	if err := local.SetRemoteDescription(*remote.LocalDescription()); err != nil {
		t.Fatal(err)
	}
}
