package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/h264reader"
)

// webrtcSession represents one viewer's WebRTC session
type webrtcSession struct {
	pc        *webrtc.PeerConnection
	videoTrk  *webrtc.TrackLocalStaticSample
	ffmpegCmd *exec.Cmd
	stop      chan struct{}
	wsMu      sync.Mutex
	lastKFReq time.Time
	cfg       ffmpegCfg
	restartMu sync.Mutex
}

type ffmpegCfg struct {
	width, height, fps, bitrateKbps int
}

// handleWebRTCSignaling handles the /ws/webrtc signaling WebSocket
func handleWebRTCSignaling(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = "Bearer " + r.Header.Get("Sec-WebSocket-Protocol")
	}
	if _, _, valid := validateToken(token); !valid {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WebRTC] upgrade err: %v", err)
		return
	}
	defer conn.Close()

	sess := &webrtcSession{stop: make(chan struct{})}
	defer sess.close()

	sendJSON := func(v interface{}) error {
		sess.wsMu.Lock()
		defer sess.wsMu.Unlock()
		return conn.WriteJSON(v)
	}

	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("[WebRTC] ws closed: %v", err)
			return
		}

		switch msg["type"] {
		case "start":
			width := intFromMap(msg, "width", 1920)
			height := intFromMap(msg, "height", 1080)
			fps := intFromMap(msg, "fps", 30)
			bitrate := intFromMap(msg, "bitrate", 4000) // kbps

			if err := sess.start(sendJSON, width, height, fps, bitrate); err != nil {
				log.Printf("[WebRTC] start err: %v", err)
				sendJSON(map[string]string{"type": "error", "error": err.Error()})
				return
			}

		case "answer":
			sdp, _ := msg["sdp"].(string)
			if sess.pc == nil {
				continue
			}
			if err := sess.pc.SetRemoteDescription(webrtc.SessionDescription{
				Type: webrtc.SDPTypeAnswer,
				SDP:  sdp,
			}); err != nil {
				log.Printf("[WebRTC] setRemoteDesc err: %v", err)
			}

		case "ice":
			cand, _ := msg["candidate"].(string)
			if sess.pc == nil || cand == "" {
				continue
			}
			sdpMid := ""
			if s, ok := msg["sdpMid"].(string); ok {
				sdpMid = s
			}
			var sdpMLineIndex *uint16
			if v, ok := msg["sdpMLineIndex"].(float64); ok {
				idx := uint16(v)
				sdpMLineIndex = &idx
			}
			_ = sess.pc.AddICECandidate(webrtc.ICECandidateInit{
				Candidate:     cand,
				SDPMid:        &sdpMid,
				SDPMLineIndex: sdpMLineIndex,
			})

		case "keyframe":
			// Client requested IDR — ffmpeg uses 1s GOP so next keyframe arrives quickly.
			// On emergency stall, restart the encoder to immediately emit SPS/PPS+IDR.
			sess.requestKeyframe()

		case "stop":
			return
		}
	}
}

func intFromMap(m map[string]interface{}, key string, def int) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return def
}

func (s *webrtcSession) start(sendJSON func(interface{}) error, width, height, fps, bitrateKbps int) error {
	// Build peer connection with public STUN
	cfg := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.cloudflare.com:3478"}},
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}

	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		},
		PayloadType: 102,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return err
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))

	pc, err := api.NewPeerConnection(cfg)
	if err != nil {
		return err
	}
	s.pc = pc

	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video", "fastremote",
	)
	if err != nil {
		return err
	}
	s.videoTrk = track
	if _, err := pc.AddTrack(track); err != nil {
		return err
	}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		msg := map[string]interface{}{
			"type":          "ice",
			"candidate":     init.Candidate,
			"sdpMid":        init.SDPMid,
			"sdpMLineIndex": init.SDPMLineIndex,
		}
		_ = sendJSON(msg)
	})

	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		log.Printf("[WebRTC] state=%s", st.String())
		if st == webrtc.PeerConnectionStateConnected {
			go func() {
				stats := pc.GetStats()
				for _, v := range stats {
					if cp, ok := v.(webrtc.ICECandidatePairStats); ok && cp.State == webrtc.StatsICECandidatePairStateSucceeded {
						log.Printf("[WebRTC] active pair: nominated=%v local=%s remote=%s", cp.Nominated, cp.LocalCandidateID, cp.RemoteCandidateID)
					}
				}
			}()
		}
		if st == webrtc.PeerConnectionStateFailed || st == webrtc.PeerConnectionStateClosed {
			select {
			case <-s.stop:
			default:
				close(s.stop)
			}
		}
	})

	// Create offer
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return err
	}

	if err := sendJSON(map[string]interface{}{
		"type": "offer",
		"sdp":  offer.SDP,
	}); err != nil {
		return err
	}

	s.cfg = ffmpegCfg{width, height, fps, bitrateKbps}
	// Start ffmpeg x11grab → H264 pipe (restarts on kill)
	go s.ffmpegLoop()

	return nil
}

func (s *webrtcSession) ffmpegLoop() {
	failStart := time.Time{}
	failCount := 0
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		runStart := time.Now()
		s.runFFmpeg(s.cfg.width, s.cfg.height, s.cfg.fps, s.cfg.bitrateKbps)
		// If encoder exits immediately (<500ms), count as failure; back off after 3 in a row.
		if time.Since(runStart) < 500*time.Millisecond {
			if failStart.IsZero() {
				failStart = time.Now()
			}
			failCount++
			if failCount >= 3 {
				log.Printf("[WebRTC] ffmpeg failing repeatedly — stopping session")
				return
			}
		} else {
			failStart = time.Time{}
			failCount = 0
		}
		select {
		case <-s.stop:
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (s *webrtcSession) runFFmpeg(width, height, fps, bitrateKbps int) {
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":1"
	}

	encoder := pickEncoder()
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "x11grab",
		"-framerate", strconv.Itoa(fps),
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-draw_mouse", "1",
		"-i", display,
		"-vf", fmt.Sprintf("scale=%d:%d:flags=fast_bilinear", width, height),
	}

	switch encoder {
	case "h264_nvenc":
		args = append(args,
			"-c:v", "h264_nvenc",
			"-preset", "llhp",
			"-rc", "cbr",
			"-b:v", fmt.Sprintf("%dk", bitrateKbps),
			"-maxrate", fmt.Sprintf("%dk", bitrateKbps),
			"-g", strconv.Itoa(fps),
			"-force_key_frames", "expr:gte(t,n_forced*1)",
			"-zerolatency", "1",
			"-delay", "0",
			"-bf", "0",
			"-profile:v", "baseline",
		)
	default:
		args = append(args,
			"-c:v", "libx264",
			"-preset", "ultrafast",
			"-tune", "zerolatency",
			"-profile:v", "baseline",
			"-b:v", fmt.Sprintf("%dk", bitrateKbps),
			"-maxrate", fmt.Sprintf("%dk", bitrateKbps),
			"-bufsize", fmt.Sprintf("%dk", bitrateKbps/2),
			"-g", strconv.Itoa(fps),
			"-force_key_frames", "expr:gte(t,n_forced*1)",
			"-bf", "0",
			"-pix_fmt", "yuv420p",
		)
	}

	args = append(args, "-f", "h264", "pipe:1")

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("[WebRTC] ffmpeg pipe err: %v", err)
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("[WebRTC] ffmpeg start err: %v", err)
		return
	}
	s.ffmpegCmd = cmd
	log.Printf("[WebRTC] ffmpeg started: encoder=%s %dx%d@%d %dkbps", encoder, width, height, fps, bitrateKbps)

	frameDur := time.Second / time.Duration(fps)
	reader, err := h264reader.NewReader(bufio.NewReaderSize(stdout, 1<<20))
	if err != nil {
		log.Printf("[WebRTC] h264reader err: %v", err)
		return
	}

	for {
		select {
		case <-s.stop:
			return
		default:
		}
		nal, err := reader.NextNAL()
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Printf("[WebRTC] nal read err: %v", err)
			return
		}
		if err := s.videoTrk.WriteSample(media.Sample{
			Data:     append([]byte{0x00, 0x00, 0x00, 0x01}, nal.Data...),
			Duration: frameDur,
		}); err != nil {
			log.Printf("[WebRTC] write sample err: %v", err)
			return
		}
	}
}

func (s *webrtcSession) requestKeyframe() {
	// No-op — ffmpeg has 1s GOP and force_key_frames every 1s.
	// Restarting ffmpeg caused visible flicker loops; rely on the implicit keyframe cadence instead.
}

func (s *webrtcSession) close() {
	if s.ffmpegCmd != nil && s.ffmpegCmd.Process != nil {
		_ = s.ffmpegCmd.Process.Kill()
	}
	if s.pc != nil {
		_ = s.pc.Close()
	}
}

func pickEncoder() string {
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err == nil && containsBytes(out, "h264_nvenc") {
		return "h264_nvenc"
	}
	return "libx264"
}

func containsBytes(b []byte, s string) bool {
	return len(b) > 0 && len(s) > 0 && indexBytes(b, s) >= 0
}

func indexBytes(b []byte, s string) int {
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == s {
			return i
		}
	}
	return -1
}

// Silence unused import warnings if signaling JSON not used
var _ = json.Marshal
