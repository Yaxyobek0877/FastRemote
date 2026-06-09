import { getToken } from './api';

export function createWebRTCSession({ videoEl, width = 1280, height = 720, fps = 30, bitrate = 2000, onState, onStats } = {}) {
  const token = getToken();
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const host = window.location.hostname === 'localhost'
    ? `${window.location.hostname}:9090`
    : `${window.location.hostname}${window.location.port ? ':' + window.location.port : ''}`;
  const url = `${proto}//${host}/ws/webrtc?token=Bearer%20${encodeURIComponent(token)}`;

  const ws = new WebSocket(url);
  ws.binaryType = 'arraybuffer';

  const pc = new RTCPeerConnection({
    iceServers: [
      { urls: 'stun:stun.cloudflare.com:3478' },
      { urls: 'stun:stun.l.google.com:19302' },
    ],
    bundlePolicy: 'max-bundle',
  });

  let receiver = null;
  let catchUpTimer = null;
  let statsTimer = null;
  let keyframeTimer = null;
  let lastFramesDecoded = 0;
  let lastStatsTs = 0;

  // Zero the jitter buffer for lowest latency (AnyDesk/Parsec-style)
  const applyLowLatency = () => {
    try {
      pc.getReceivers().forEach((r) => {
        if (r.track && r.track.kind === 'video') {
          receiver = r;
          if ('playoutDelayHint' in r) r.playoutDelayHint = 0;
          if ('jitterBufferTarget' in r) r.jitterBufferTarget = 0;
        }
      });
    } catch {}
  };

  pc.ontrack = (ev) => {
    if (videoEl && ev.streams && ev.streams[0]) {
      videoEl.srcObject = ev.streams[0];
      videoEl.playsInline = true;
      videoEl.muted = true;
      videoEl.autoplay = true;
      try { videoEl.play(); } catch {}
    }
    applyLowLatency();
    startCatchUpLoop();
    startStatsLoop();
  };

  pc.onicecandidate = (ev) => {
    if (ev.candidate && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({
        type: 'ice',
        candidate: ev.candidate.candidate,
        sdpMid: ev.candidate.sdpMid,
        sdpMLineIndex: ev.candidate.sdpMLineIndex,
      }));
    }
  };

  pc.onconnectionstatechange = () => {
    if (onState) onState(pc.connectionState);
  };

  ws.onopen = () => {
    ws.send(JSON.stringify({ type: 'start', width, height, fps, bitrate }));
  };

  ws.onmessage = async (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.type === 'offer') {
      await pc.setRemoteDescription({ type: 'offer', sdp: msg.sdp });
      const answer = await pc.createAnswer();
      await pc.setLocalDescription(answer);
      ws.send(JSON.stringify({ type: 'answer', sdp: answer.sdp }));
    } else if (msg.type === 'ice') {
      try {
        await pc.addIceCandidate({
          candidate: msg.candidate,
          sdpMid: msg.sdpMid,
          sdpMLineIndex: msg.sdpMLineIndex,
        });
      } catch (e) { /* ignore */ }
    } else if (msg.type === 'error') {
      console.error('[WebRTC] server error:', msg.error);
    }
  };

  ws.onerror = (e) => console.error('[WebRTC] ws err', e);
  ws.onclose = () => { if (onState) onState('closed'); };

  // === Catch-up loop: if buffered frames lag behind, jump to latest ===
  // Runs at 400ms for micro-adjustments, plus hard check at 3s
  const SOFT_THRESHOLD = 0.15; // 150ms — start speeding up
  const HARD_THRESHOLD = 0.5;  // 500ms — jump
  const EMERGENCY_THRESHOLD = 3.0; // 3s — force keyframe + jump

  function startCatchUpLoop() {
    stopCatchUpLoop();
    catchUpTimer = setInterval(() => {
      if (!videoEl || videoEl.readyState < 2) return;
      const buf = videoEl.buffered;
      if (!buf || buf.length === 0) return;
      const latestEnd = buf.end(buf.length - 1);
      const delay = latestEnd - videoEl.currentTime;

      if (delay > EMERGENCY_THRESHOLD) {
        // >3s stuck: jump to latest (keyframe arrives every 1s from server)
        try { videoEl.currentTime = Math.max(0, latestEnd - 0.05); } catch {}
        videoEl.playbackRate = 1.0;
      } else if (delay > HARD_THRESHOLD) {
        // >500ms: jump to latest
        try { videoEl.currentTime = Math.max(0, latestEnd - 0.05); } catch {}
        videoEl.playbackRate = 1.0;
      } else if (delay > SOFT_THRESHOLD) {
        // >150ms: speed up slightly (smooth catch-up, AnyDesk-style)
        videoEl.playbackRate = 1.15;
      } else {
        videoEl.playbackRate = 1.0;
      }
    }, 400);
  }
  function stopCatchUpLoop() {
    if (catchUpTimer) { clearInterval(catchUpTimer); catchUpTimer = null; }
  }

  // === Stats loop (every 3s): expose latency/fps/bitrate, detect stalls ===
  function startStatsLoop() {
    stopStatsLoop();
    statsTimer = setInterval(async () => {
      if (!pc) return;
      try {
        const stats = await pc.getStats(null);
        let framesDecoded = 0, bytesReceived = 0, jitter = 0, frameHeight = 0, frameWidth = 0, packetsLost = 0, fps = 0;
        stats.forEach((r) => {
          if (r.type === 'inbound-rtp' && r.kind === 'video') {
            framesDecoded = r.framesDecoded || 0;
            bytesReceived = r.bytesReceived || 0;
            jitter = r.jitter || 0;
            frameHeight = r.frameHeight || 0;
            frameWidth = r.frameWidth || 0;
            packetsLost = r.packetsLost || 0;
            fps = r.framesPerSecond || 0;
          }
        });
        const now = performance.now();
        const dt = lastStatsTs ? (now - lastStatsTs) / 1000 : 0;
        const framesDelta = framesDecoded - lastFramesDecoded;
        lastFramesDecoded = framesDecoded;
        lastStatsTs = now;

        // Stall detection — logged only; ffmpeg emits keyframe every 1s naturally
        if (dt > 0 && framesDelta === 0 && framesDecoded > 0) {
          console.warn('[WebRTC] stalled: no frames decoded in', dt.toFixed(1), 's');
        }

        if (onStats) {
          onStats({
            fps: Math.round(fps),
            width: frameWidth,
            height: frameHeight,
            jitterMs: Math.round(jitter * 1000),
            packetsLost,
            bytesPerSec: dt ? Math.round((bytesReceived - (onStats._lastBytes || 0)) / dt) : 0,
            bufferDelayMs: videoEl && videoEl.buffered.length
              ? Math.round((videoEl.buffered.end(videoEl.buffered.length - 1) - videoEl.currentTime) * 1000)
              : 0,
          });
          onStats._lastBytes = bytesReceived;
        }
      } catch {}
    }, 1500);
  }
  function stopStatsLoop() {
    if (statsTimer) { clearInterval(statsTimer); statsTimer = null; }
  }

  return {
    close() {
      stopCatchUpLoop();
      stopStatsLoop();
      if (keyframeTimer) clearInterval(keyframeTimer);
      try { ws.close(); } catch {}
      try { pc.close(); } catch {}
      if (videoEl) videoEl.srcObject = null;
    },
  };
}
