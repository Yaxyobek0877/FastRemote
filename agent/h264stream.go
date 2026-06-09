package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/pion/webrtc/v4/pkg/media/h264reader"
)

// h264Streamer — ffmpeg x11grab orqali ekranni H.264 ga kodlab, har bir kadrni
// (access unit) sink callback'ga uzatadi. WebRTC'dan farqli — bu NAL'larni oddiy
// WebSocket orqali yuborish uchun, ya'ni cloudflared tunnel orqali ham ishlaydi.
type h264Streamer struct {
	cmd  *exec.Cmd
	stop chan struct{}
	once sync.Once
}

// startH264Stream — oqimni boshlaydi. sink(au, key): au — Annex-B formatdagi bitta
// to'liq kadr, key — keyframe (IDR) ekanligi.
func startH264Stream(width, height, fps, bitrateKbps int, sink func(au []byte, key bool)) *h264Streamer {
	s := &h264Streamer{stop: make(chan struct{})}
	go s.loop(width, height, fps, bitrateKbps, sink)
	return s
}

func (s *h264Streamer) close() {
	s.once.Do(func() { close(s.stop) })
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

func (s *h264Streamer) loop(width, height, fps, bitrateKbps int, sink func([]byte, bool)) {
	fails := 0
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		start := time.Now()
		s.run(width, height, fps, bitrateKbps, sink)
		if time.Since(start) < 700*time.Millisecond {
			fails++
			if fails >= 3 {
				log.Printf("[H264] ffmpeg qayta-qayta ishlamayapti — to'xtatildi")
				return
			}
		} else {
			fails = 0
		}
		select {
		case <-s.stop:
			return
		case <-time.After(400 * time.Millisecond):
		}
	}
}

func (s *h264Streamer) run(width, height, fps, bitrateKbps int, sink func([]byte, bool)) {
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":0"
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
			"-c:v", "h264_nvenc", "-preset", "llhp",
			"-rc", "cbr", "-b:v", fmt.Sprintf("%dk", bitrateKbps),
			"-maxrate", fmt.Sprintf("%dk", bitrateKbps),
			"-g", strconv.Itoa(fps), "-zerolatency", "1", "-delay", "0",
			"-bf", "0", "-profile:v", "baseline",
		)
	default:
		args = append(args,
			"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
			"-profile:v", "baseline",
			"-b:v", fmt.Sprintf("%dk", bitrateKbps),
			"-maxrate", fmt.Sprintf("%dk", bitrateKbps),
			"-bufsize", fmt.Sprintf("%dk", bitrateKbps/4),
			"-g", strconv.Itoa(fps), "-keyint_min", strconv.Itoa(fps),
			"-bf", "0", "-pix_fmt", "yuv420p",
		)
	}
	args = append(args, "-f", "h264", "pipe:1")

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("[H264] pipe xato: %v", err)
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("[H264] ffmpeg start xato: %v", err)
		return
	}
	s.cmd = cmd
	log.Printf("[H264] ffmpeg boshlandi: %s %dx%d@%d %dkbps (display=%s)", encoder, width, height, fps, bitrateKbps, display)

	reader, err := h264reader.NewReader(bufio.NewReaderSize(stdout, 1<<20))
	if err != nil {
		return
	}

	// Access-unit (kadr) yig'ish: yangi VCL NAL (1/5) keldi va bufferda allaqachon
	// VCL bo'lsa — oldingi kadrni flush qilamiz.
	var au []byte
	hasVCL := false
	isKey := false
	flush := func() {
		if len(au) == 0 {
			return
		}
		buf := make([]byte, len(au))
		copy(buf, au)
		sink(buf, isKey)
		au = au[:0]
		hasVCL = false
		isKey = false
	}
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		nal, err := reader.NextNAL()
		if err != nil {
			flush()
			return
		}
		if len(nal.Data) == 0 {
			continue
		}
		t := nal.Data[0] & 0x1F
		vcl := t == 1 || t == 5
		if vcl && hasVCL {
			flush()
		}
		au = append(au, 0x00, 0x00, 0x00, 0x01)
		au = append(au, nal.Data...)
		if vcl {
			hasVCL = true
		}
		if t == 5 {
			isKey = true
		}
	}
}
