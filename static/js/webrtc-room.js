import { listen as pageListen, onPageCleanup } from './lib/page-scope.js';
import { createSignalingQueue } from './lib/webrtc-signaling-queue.js';
// webrtc-room.js — joins a producer-v2 live session over the SFU.
//
// Initializes controls from a #webrtc-room element carrying data-room (show-note
// id) and data-role ("host" may publish camera/mic, "viewer" only subscribes).
// Host workspaces do not acquire devices or open signaling until Start call is
// clicked. Standalone production monitors may opt into subscription-only auto
// connect with data-auto-connect="true".
//
// Camera identity: the SFU relays a stable stream→host mapping ("meta" events).
// Self is keyed by its real localStream.id (the same id the SFU forwards to every
// other peer), so webcam slotting is consistent across the producer, other hosts,
// and the viewer. The scene engine (three-scene.js) reads window.__rewindCams.

(function () {
  const root = document.getElementById('webrtc-room');
  if (!root) return;

  const room = root.dataset.room;
  const role = root.dataset.role || 'viewer';
  const selfId = root.dataset.userId || '';
  const selfName = root.dataset.username || '';
  const grid = document.querySelector('[data-call-grid]') || document.getElementById('cam-grid');
  const startButton = document.querySelector('[data-call-start]');
  const leaveButton = document.querySelector('[data-call-leave]');
  const micButton = document.querySelector('[data-call-mic]');
  const cameraButton = document.querySelector('[data-call-camera]');
  const screenButton = document.querySelector('[data-call-screen]');
  const idle = document.querySelector('[data-call-idle]');
  const controls = document.querySelector('[data-call-controls]');
  const stateLabel = document.querySelector('[data-call-state]');
  if (!room) return;

  let pc = null;
  let ws = null;
  let localStream = null;
  const tiles = new Map(); // stream id -> { tile, video, tag }
  const audios = new Map(); // stream id -> <audio> (remote sound; video meshes are muted)
  let gesturePrompted = false;
  const publisherKey = `rewind:show-publisher:${room}`;
  const windowID = (crypto.randomUUID && crypto.randomUUID()) || `${Date.now()}-${Math.random()}`;
  let publishes = false;
  let publisherTimer = null;
  let joined = false;
  let joining = false;

  async function loadICEConfig(signalRole) {
    const response = await fetch(`/api/show-notes/${encodeURIComponent(room)}/ice?role=${encodeURIComponent(signalRole)}`, {
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    });
    if (!response.ok) throw new Error(`ICE service unavailable (${response.status})`);
    const config = await response.json();
    if (!Array.isArray(config.iceServers) || config.iceServers.length === 0) {
      throw new Error('ICE service returned no servers');
    }
    return config;
  }

  function readPublisher() {
    try { return JSON.parse(localStorage.getItem(publisherKey) || 'null'); } catch (_) { return null; }
  }

  function claimPublisher(force) {
    if (role !== 'host') return false;
    const current = readPublisher();
    const now = Date.now();
    if (!force && current && current.id !== windowID && current.expires > now) return false;
    localStorage.setItem(publisherKey, JSON.stringify({ id: windowID, expires: now + 5000 }));
    publishes = true;
    return true;
  }

  function stopPublishing() {
    publishes = false;
    if (localStream) {
      localStream.getTracks().forEach((track) => track.stop());
      removeTile(localStream.id);
      localStream = null;
    }
    stopScreenShare();
  }

  const forcePublisher = new URLSearchParams(location.search).get('publisher') === 'take';

  function startPublisherLease() {
    if (publisherTimer || !publishes) return;
    publisherTimer = window.setInterval(() => {
      const current = readPublisher();
      if (current && current.id !== windowID && current.expires > Date.now()) {
        leaveCall(false);
        return;
      }
      localStorage.setItem(publisherKey, JSON.stringify({ id: windowID, expires: Date.now() + 5000 }));
    }, 1800);
  }

  function stopPublisherLease(release = true) {
    if (publisherTimer) clearInterval(publisherTimer);
    publisherTimer = null;
    if (!release) return;
    const current = readPublisher();
    if (current && current.id === windowID) localStorage.removeItem(publisherKey);
  }

  pageListen(window, 'storage', (event) => {
    if (event.key !== publisherKey || !publishes) return;
    const current = readPublisher();
    if (current && current.id !== windowID) leaveCall(false);
  });
  pageListen(window, 'pagehide', () => leaveCall());
  onPageCleanup(() => leaveCall());

  // Registry consumed by the Three.js scene engine (three-scene.js):
  //   videos: streamId -> <video>  (composited as VideoTexture meshes)
  //   meta:   streamId -> { userId, username, self }  (stable host identity)
  window.__rewindCams = window.__rewindCams || { videos: new Map(), meta: new Map() };
  const cams = window.__rewindCams;
  if (!cams.videos) cams.videos = new Map();
  if (!cams.meta) cams.meta = new Map();

  function tileFor(streamId, label) {
    if (tiles.has(streamId)) return tiles.get(streamId);
    const tile = document.createElement('div');
    tile.className =
      'relative bg-black/70 border border-white/20 overflow-hidden pointer-events-auto min-h-0';
    const video = document.createElement('video');
    video.autoplay = true;
    video.playsInline = true;
    // The video element only feeds the VideoTexture — keep it muted so it always
    // autoplays (muted autoplay is never blocked). Sound comes from a separate
    // <audio> element per remote stream (see attachAudio).
    video.muted = true;
    video.className = 'w-full h-full object-cover';
    tile.appendChild(video);
    const tag = document.createElement('div');
    tag.className = 'absolute bottom-0 left-0 text-[10px] px-1 bg-black/70 text-white/80';
    tag.textContent = label || '';
    tag.style.display = label ? 'block' : 'none';
    tile.appendChild(tag);
    if (grid) grid.appendChild(tile);
    const entry = { tile, video, tag };
    tiles.set(streamId, entry);
    cams.videos.set(streamId, video);
    return entry;
  }

  function removeTile(streamId) {
    const e = tiles.get(streamId);
    if (e) {
      e.tile.remove();
      tiles.delete(streamId);
      cams.videos.delete(streamId);
      cams.meta.delete(streamId);
    }
    detachAudio(streamId);
  }

  // playSafe attempts playback; if the browser blocks it (no user gesture, e.g. a
  // fresh viewer/OBS tab), it shows a one-time click-to-start affordance.
  function playSafe(el) {
    if (!el) return;
    const p = el.play();
    if (p && p.catch) p.catch(() => promptGesture());
  }

  // promptGesture shows a small corner button; one click resumes all media. OBS
  // browser sources autoplay without a gesture, so this only appears in plain tabs.
  function promptGesture() {
    if (gesturePrompted) return;
    gesturePrompted = true;
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.textContent = '▶ Click to start audio';
    btn.className =
      'fixed bottom-3 left-3 z-50 px-3 py-2 text-sm font-mono bg-black/80 text-white border border-white/30';
    btn.addEventListener('click', () => {
      tiles.forEach((t) => playSafe(t.video));
      audios.forEach((a) => playSafe(a));
      btn.remove();
    });
    (document.body || document.documentElement).appendChild(btn);
  }

  // attachAudio routes a remote stream's sound through a dedicated unmuted <audio>
  // element (the video meshes are muted). Skips streams with no audio track.
  function attachAudio(stream) {
    if (!stream.getAudioTracks || stream.getAudioTracks().length === 0) return;
    let a = audios.get(stream.id);
    if (!a) {
      a = document.createElement('audio');
      a.autoplay = true;
      a.style.display = 'none';
      (document.body || document.documentElement).appendChild(a);
      audios.set(stream.id, a);
    }
    if (a.srcObject !== stream) a.srcObject = stream;
    playSafe(a);
  }

  function detachAudio(streamId) {
    const a = audios.get(streamId);
    if (a) {
      try {
        a.pause();
      } catch (_) {}
      a.srcObject = null;
      a.remove();
      audios.delete(streamId);
    }
  }

  // applyIdentities updates the stream→host map from an SFU "meta" message and
  // refreshes tile labels. The self entry's `self` flag is preserved.
  function applyIdentities(list) {
    if (!Array.isArray(list)) return;
    for (const m of list) {
      if (!m || !m.streamId) continue;
      const prev = cams.meta.get(m.streamId) || {};
      cams.meta.set(m.streamId, {
        userId: m.userId || prev.userId || '',
        username: m.username || prev.username || '',
        kind: m.kind || prev.kind || 'camera',
        self: !!prev.self,
      });
      const t = tiles.get(m.streamId);
      if (t && t.tag && m.username) {
        t.tag.textContent = m.username;
        t.tag.style.display = 'block';
      }
    }
  }

  function send(obj) {
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(obj));
  }

  async function startConnection() {
    const config = await loadICEConfig(role === 'host' && publishes ? 'host' : 'viewer');
    pc = new RTCPeerConnection(config);

    pc.ontrack = (ev) => {
      const stream = ev.streams && ev.streams[0];
      if (!stream) return;
      const meta = cams.meta.get(stream.id);
      const entry = tileFor(stream.id, meta && meta.username ? meta.username : '');
      if (entry.video.srcObject !== stream) {
        entry.video.srcObject = stream;
        playSafe(entry.video);
      }
      attachAudio(stream); // audio + video can arrive as separate ontrack events
      stream.addEventListener('addtrack', () => attachAudio(stream));
      stream.addEventListener('removetrack', () => {
        if (stream.getTracks().length === 0) removeTile(stream.id);
        else attachAudio(stream);
      });
    };

    pc.onicecandidate = (ev) => {
      if (ev.candidate) send({ event: 'candidate', data: JSON.stringify(ev.candidate) });
    };

    if (role === 'host' && publishes) {
      try {
        localStream = await navigator.mediaDevices.getUserMedia({ video: true, audio: true });
        // Key self by the real stream id (matches what the SFU forwards to peers)
        // and mute the local tile so there's no echo. Mark self in meta up-front,
        // before signaling, so binding resolves it immediately.
        const self = tileFor(localStream.id, selfName || 'you');
        self.video.srcObject = localStream;
        playSafe(self.video);
        cams.meta.set(localStream.id, { userId: selfId, username: selfName, kind: 'camera', self: true });
        localStream.getTracks().forEach((t) => pc.addTrack(t, localStream));
      } catch (e) {
        console.warn('webrtc-room: getUserMedia failed', e);
      }
    }

    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    const signalRole = role === 'host' && publishes ? 'host' : 'viewer';
    ws = new WebSocket(`${proto}://${location.host}/api/show-notes/${room}/signal?role=${signalRole}`);

    const signalQueue = createSignalingQueue({
      onOffer: async (msg) => {
        await pc.setRemoteDescription(JSON.parse(msg.data));
        const answer = await pc.createAnswer();
        await pc.setLocalDescription(answer);
        send({ event: 'answer', data: JSON.stringify(answer) });
      },
      onCandidate: async (msg) => {
        try {
          await pc.addIceCandidate(JSON.parse(msg.data));
        } catch (_) {}
      },
      onMeta: async (msg) => {
        try {
          applyIdentities(JSON.parse(msg.data));
        } catch (_) {}
      },
      onError: (error) => console.warn('webrtc-room: signaling queue error', error),
    });
    ws.onmessage = (ev) => { signalQueue.push(ev.data); };

    ws.onclose = () => {
      if (pc) {
        pc.close();
        pc = null;
      }
    };
  }

  function updateCallUI(message) {
    if (idle) {
      idle.hidden = joined || joining;
      idle.classList.toggle('hidden', joined || joining);
    }
    if (controls) {
      controls.hidden = !joined;
      controls.classList.toggle('hidden', !joined);
    }
    if (startButton) startButton.disabled = joining;
    if (stateLabel) stateLabel.textContent = message || (joining ? 'Joining call…' : joined ? 'In call' : 'Call is off');
  }

  async function joinCall() {
    if (joined || joining) return joined;
    joining = true;
    updateCallUI('Joining call…');
    if (role === 'host' && !claimPublisher(forcePublisher)) {
      joining = false;
      updateCallUI('Call is active in another window');
      return false;
    }
    try {
      joined = true;
      if (publishes) startPublisherLease();
      await startConnection();
      updateCallUI('In call');
      return true;
    } catch (error) {
      console.warn('webrtc-room: failed to join', error);
      leaveCall();
      updateCallUI('Could not start call');
      return false;
    } finally {
      joining = false;
    }
  }

  function leaveCall(releasePublisher = true) {
    joined = false;
    joining = false;
    if (ws) {
      try { ws.close(); } catch (_) {}
      ws = null;
    }
    if (pc) {
      try { pc.close(); } catch (_) {}
      pc = null;
    }
    stopPublishing();
    stopPublisherLease(releasePublisher);
    for (const streamID of Array.from(tiles.keys())) removeTile(streamID);
    for (const streamID of Array.from(audios.keys())) detachAudio(streamID);
    updateCallUI();
  }

  function toggleTrack(kind, button) {
    if (!localStream) return false;
    const tracks = kind === 'audio' ? localStream.getAudioTracks() : localStream.getVideoTracks();
    if (!tracks.length) return false;
    const enabled = !tracks[0].enabled;
    tracks.forEach((track) => { track.enabled = enabled; });
    if (button) {
      button.dataset.muted = enabled ? 'false' : 'true';
      button.classList.toggle('is-active', !enabled);
      button.setAttribute('aria-pressed', enabled ? 'false' : 'true');
    }
    return enabled;
  }

  function toggleMicrophone() { return toggleTrack('audio', micButton); }
  function toggleCamera() { return toggleTrack('video', cameraButton); }

  // ── Screen share ───────────────────────────────────────────────────────────
  // A host shares a screen/window via a SECOND, publish-only SFU connection (its
  // own PC + WS with kind=screen). This reuses the plain single-stream publish
  // path — no multi-track renegotiation against the SFU (which is the offerer).
  let screenPc = null;
  let screenWs = null;
  let screenStream = null;

  function mergeDS(o) {
    const ds = window.__dsAPI;
    if (ds && ds.mergePatch) ds.mergePatch(o);
  }

  async function startScreenShare() {
    if (!publishes) return false;
    if (screenStream) return true;
    let config;
    try {
      config = await loadICEConfig('host');
    } catch (error) {
      console.warn('screen-share: ICE configuration failed', error);
      return false;
    }
    let stream;
    try {
      stream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: true });
    } catch (_) {
      return false; // user dismissed the picker
    }
    screenStream = stream;

    // Show it locally right away (self screen), keyed by its real stream id so it
    // slots consistently everywhere. Muted locally — sound reaches other hosts +
    // viewers via the SFU; playing our own tab audio back would echo.
    const st = tileFor(stream.id, (selfName || 'you') + ' — screen');
    st.video.srcObject = stream;
    playSafe(st.video);
    cams.meta.set(stream.id, { userId: selfId, username: selfName, kind: 'screen', self: true });

    screenPc = new RTCPeerConnection(config);
    screenPc.onicecandidate = (ev) => {
      if (ev.candidate && screenWs && screenWs.readyState === WebSocket.OPEN)
        screenWs.send(JSON.stringify({ event: 'candidate', data: JSON.stringify(ev.candidate) }));
    };
    stream.getTracks().forEach((tr) => screenPc.addTrack(tr, stream));
    // The browser's own "Stop sharing" bar ends the track — mirror that to us.
    stream.getVideoTracks().forEach((tr) => tr.addEventListener('ended', stopScreenShare));

    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    screenWs = new WebSocket(`${proto}://${location.host}/api/show-notes/${room}/signal?role=host&kind=screen`);
    const screenSignalQueue = createSignalingQueue({
      onOffer: async (msg) => {
        await screenPc.setRemoteDescription(JSON.parse(msg.data));
        const ans = await screenPc.createAnswer();
        await screenPc.setLocalDescription(ans);
        screenWs.send(JSON.stringify({ event: 'answer', data: JSON.stringify(ans) }));
      },
      onCandidate: async (msg) => {
        try {
          await screenPc.addIceCandidate(JSON.parse(msg.data));
        } catch (_) {}
      },
      onError: (error) => console.warn('screen-share: signaling queue error', error),
    });
    screenWs.onmessage = (ev) => { screenSignalQueue.push(ev.data); };
    screenWs.onclose = () => {
      if (screenPc) {
        screenPc.close();
        screenPc = null;
      }
    };

    mergeDS({ screenSharing: true });
    if (window.__sceneEditor && window.__sceneEditor.addScreenForSelf) window.__sceneEditor.addScreenForSelf();
    return true;
  }

  function stopScreenShare() {
    if (screenStream) {
      const id = screenStream.id;
      screenStream.getTracks().forEach((t) => {
        try {
          t.stop();
        } catch (_) {}
      });
      screenStream = null;
      removeTile(id);
    }
    if (screenWs) {
      try {
        screenWs.close();
      } catch (_) {}
      screenWs = null;
    }
    if (screenPc) {
      try {
        screenPc.close();
      } catch (_) {}
      screenPc = null;
    }
    mergeDS({ screenSharing: false });
  }

  window.__rewindRoom = {
    joinCall,
    leaveCall,
    toggleMicrophone,
    toggleCamera,
    startScreenShare,
    stopScreenShare,
    isJoined: () => joined,
  };

  startButton?.addEventListener('click', joinCall);
  leaveButton?.addEventListener('click', () => leaveCall());
  micButton?.addEventListener('click', toggleMicrophone);
  cameraButton?.addEventListener('click', toggleCamera);
  screenButton?.addEventListener('click', startScreenShare);
  updateCallUI();
  if (root.dataset.autoConnect === 'true') joinCall();
})();
