package capture

import (
	"io"
	"log"
	"os/exec"
	"sync"
	"time"
)

// AudioCapturer captures system audio using PulseAudio or PipeWire
type AudioCapturer struct {
	mu        sync.Mutex
	streaming bool
	cmd       *exec.Cmd
	stopChan  chan struct{}
	chunkChan chan []byte
	available bool
	backend   string // "parec" or "pw-record"
}

// NewAudioCapturer creates a new audio capturer
func NewAudioCapturer() *AudioCapturer {
	ac := &AudioCapturer{
		chunkChan: make(chan []byte, 10),
	}

	// Detect available audio capture backend
	if _, err := exec.LookPath("parec"); err == nil {
		ac.available = true
		ac.backend = "parec"
		log.Println("[Audio] Found PulseAudio (parec) backend")
	} else if _, err := exec.LookPath("pw-record"); err == nil {
		ac.available = true
		ac.backend = "pw-record"
		log.Println("[Audio] Found PipeWire (pw-record) backend")
	} else {
		ac.available = false
		log.Println("[Audio] No audio capture backend found (install pulseaudio-utils or pipewire)")
	}

	return ac
}

// Chunks returns the channel that receives raw PCM audio chunks
func (ac *AudioCapturer) Chunks() <-chan []byte {
	return ac.chunkChan
}

// IsAvailable returns whether audio capture is supported
func (ac *AudioCapturer) IsAvailable() bool {
	return ac.available
}

// Start begins audio capture
func (ac *AudioCapturer) Start() {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if ac.streaming || !ac.available {
		return
	}

	ac.streaming = true
	ac.stopChan = make(chan struct{})
	go ac.captureLoop()
	log.Printf("[Audio] Started capture using %s", ac.backend)
}

// Stop halts audio capture
func (ac *AudioCapturer) Stop() {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if !ac.streaming {
		return
	}

	ac.streaming = false
	close(ac.stopChan)

	if ac.cmd != nil && ac.cmd.Process != nil {
		ac.cmd.Process.Kill()
		ac.cmd = nil
	}

	log.Println("[Audio] Stopped capture")
}

// IsStreaming returns whether audio capture is active
func (ac *AudioCapturer) IsStreaming() bool {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.streaming
}

func (ac *AudioCapturer) captureLoop() {
	for {
		select {
		case <-ac.stopChan:
			return
		default:
		}

		// Find the monitor source for system audio
		var cmd *exec.Cmd

		switch ac.backend {
		case "parec":
			// Capture system audio output (monitor source)
			// Format: signed 16-bit little-endian, mono, 22050 Hz (good balance of quality/bandwidth)
			cmd = exec.Command("parec",
				"--format=s16le",
				"--rate=22050",
				"--channels=1",
				"--latency-msec=50",
				"--device=@DEFAULT_MONITOR@",
			)
		case "pw-record":
			// PipeWire capture
			cmd = exec.Command("pw-record",
				"--format=s16",
				"--rate=22050",
				"--channels=1",
				"--target=@DEFAULT_MONITOR@",
				"-",
			)
		default:
			return
		}

		ac.mu.Lock()
		ac.cmd = cmd
		ac.mu.Unlock()

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("[Audio] Failed to create stdout pipe: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		if err := cmd.Start(); err != nil {
			log.Printf("[Audio] Failed to start %s: %v", ac.backend, err)
			time.Sleep(2 * time.Second)
			continue
		}

		log.Printf("[Audio] %s process started (PID %d)", ac.backend, cmd.Process.Pid)

		// Read audio data in chunks
		// 22050 Hz * 2 bytes * 1 channel = 44100 bytes/sec
		// 50ms chunks = 2205 bytes ≈ 2048 bytes for nice binary alignment
		buf := make([]byte, 2048)

		for {
			select {
			case <-ac.stopChan:
				cmd.Process.Kill()
				return
			default:
			}

			n, err := stdout.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])

				select {
				case ac.chunkChan <- chunk:
				default:
					// Drop if channel is full
				}
			}

			if err != nil {
				if err != io.EOF {
					log.Printf("[Audio] Read error: %v", err)
				}
				break
			}
		}

		cmd.Wait()

		// Check if we should restart
		ac.mu.Lock()
		shouldRestart := ac.streaming
		ac.mu.Unlock()

		if !shouldRestart {
			return
		}

		log.Println("[Audio] Process exited, restarting...")
		time.Sleep(1 * time.Second)
	}
}
