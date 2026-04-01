package netease

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type RecognizeService struct {
	serviceURL string
	cmd        *exec.Cmd
	client     *http.Client
	mu         sync.Mutex
	started    bool
}

type RecognizeResult struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    *struct {
		Result []struct {
			Song struct {
				ID      int    `json:"id"`
				Name    string `json:"name"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				Album struct {
					Name string `json:"name"`
				} `json:"album"`
			} `json:"song"`
		} `json:"result"`
	} `json:"data"`
}

func NewRecognizeService(port int) *RecognizeService {
	if port == 0 {
		port = 3737
	}
	return &RecognizeService{
		serviceURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (s *RecognizeService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	serviceDir := filepath.Join("/app", "plugins", "netease", "recognize", "service")

	if _, err := os.Stat(filepath.Join(serviceDir, "node_modules")); os.IsNotExist(err) {
		fallbackDir := filepath.Join("plugins", "netease", "recognize", "service")
		if _, fallbackErr := os.Stat(filepath.Join(fallbackDir, "node_modules")); os.IsNotExist(fallbackErr) {
			return fmt.Errorf("node_modules not found in %s or %s, please run: cd %s && npm install", serviceDir, fallbackDir, fallbackDir)
		}
		serviceDir = fallbackDir
	}

	s.cmd = exec.CommandContext(ctx, "node", "server.js")
	s.cmd.Dir = serviceDir
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start recognition service: %w", err)
	}

	s.started = true

	if err := s.waitForReady(ctx, 10*time.Second); err != nil {
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
			_ = s.cmd.Wait()
		}
		s.started = false
		return err
	}

	return nil
}

func (s *RecognizeService) waitForReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return errors.New("timeout waiting for recognition service to start")
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, "GET", s.serviceURL+"/health", nil)
			if err != nil {
				continue
			}
			resp, err := s.client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == 200 {
					return nil
				}
			}
		}
	}
}

func (s *RecognizeService) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started || s.cmd == nil || s.cmd.Process == nil {
		return nil
	}

	if err := s.cmd.Process.Signal(os.Interrupt); err != nil {
		return s.cmd.Process.Kill()
	}

	done := make(chan error, 1)
	go func() {
		done <- s.cmd.Wait()
	}()

	select {
	case <-time.After(5 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	case <-done:
	}

	s.started = false
	return nil
}

func (s *RecognizeService) Recognize(ctx context.Context, audioData []byte) (*RecognizeResult, error) {
	if !s.started {
		return nil, errors.New("recognition service not started")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.serviceURL+"/recognize", bytes.NewReader(audioData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call recognition service: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("recognition service returned status %d", resp.StatusCode)
	}

	var result RecognizeResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}
