package netease

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"sync"
	"time"

	embind "github.com/jerbob92/wazero-emscripten-embind"
	"github.com/tetratelabs/wazero"
)

// afpWasm is the fingerprint encoder compiled into the binary. The module is
// loaded once at startup via wazero; no external file is required at runtime.
//
//go:embed recognize/wasm/afp.wasm
var afpWasm []byte

// afp.wasm fingerprint parameters, lifted verbatim from the upstream
// ncm-audio-recognize JS reference (sandbox.bundle.cjs):
//   - source audio is decoded to mono float32 PCM at 48kHz
//   - the encoder downsamples 48kHz -> 8kHz (stride 6) and takes a 6-second
//     window starting at the 4-second mark
//   - fp[i] = pcm48k[i*6 + 4*48000], i in [0, 6*8000)
//
// The fingerprint window therefore needs at least 10 seconds of 48kHz audio.
const (
	afpSampleRate   = 48000
	afpResampleHz   = 8000
	afpDurationSec  = 6
	afpFromSec      = 4
	afpStride       = afpSampleRate / afpResampleHz  // 6
	afpFingerprintN = afpDurationSec * afpResampleHz // 48000 samples
	afpFromSamples  = afpFromSec * afpSampleRate     // 192000
	afpMinSamples   = afpFromSamples + (afpFingerprintN-1)*afpStride + 1
)

// netease audio-match API, mirroring the headers/params from the JS reference.
const (
	neteaseMatchURL = "https://interface.music.163.com/api/music/audio/match"
	recognizeUA     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/102.0.0.0 Safari/537.36"
	recognizeOrigin = "chrome-extension://pgphbbekcgpfaekhcbjamjjkegcclhhd"
)

// RecognizeService encodes an audio fingerprint with the embedded afp.wasm
// module (driven purely in Go via wazero + embind, no Node.js) and matches it
// against the NetEase Cloud Music audio-match API.
type RecognizeService struct {
	client *http.Client

	mu      sync.Mutex // serializes WASM calls; the embind engine holds shared state
	started bool

	runtime wazero.Runtime
	engine  embind.Engine
	ctx     context.Context // base context the module was instantiated with
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

// NewRecognizeService constructs the service. The port argument is retained for
// configuration compatibility but is no longer used (the recognizer runs
// in-process and no longer starts an HTTP sidecar).
func NewRecognizeService(_ int) *RecognizeService {
	return &RecognizeService{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Start loads and instantiates the afp.wasm module once. The provided context
// is used as the base context for the WASM runtime lifetime.
func (s *RecognizeService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	wasmBytes, err := loadAFPWasm()
	if err != nil {
		return err
	}

	// Use a background-rooted context for the runtime lifetime so the module
	// is not torn down when the caller's startup context is cancelled.
	runtimeCtx := context.WithoutCancel(ctx)

	r := wazero.NewRuntimeWithConfig(runtimeCtx, wazero.NewRuntimeConfig())

	compiled, err := r.CompileModule(runtimeCtx, wasmBytes)
	if err != nil {
		_ = r.Close(runtimeCtx)
		return fmt.Errorf("recognize: compile afp.wasm: %w", err)
	}

	engine := embind.CreateEngine(embind.NewConfig())

	// The afp.wasm imports its embind/emscripten glue under module name "a"
	// with minified, pre-isAsync names; the custom exporter maps them.
	builder := r.NewHostModuleBuilder("a")
	exporter := embind.NewAFPFunctionExporter(engine)
	if err := exporter.ExportFunctions(builder); err != nil {
		_ = r.Close(runtimeCtx)
		return fmt.Errorf("recognize: export afp host module: %w", err)
	}
	if _, err := builder.Instantiate(runtimeCtx); err != nil {
		_ = r.Close(runtimeCtx)
		return fmt.Errorf("recognize: instantiate afp host module: %w", err)
	}

	engineCtx := engine.Attach(runtimeCtx)

	// The module has no _start/_initialize; instantiate without start
	// functions and run __wasm_call_ctors (export "C") manually, which
	// triggers all embind type registrations.
	moduleConfig := wazero.NewModuleConfig().WithStartFunctions().WithName("")
	mod, err := r.InstantiateModule(engineCtx, compiled, moduleConfig)
	if err != nil {
		_ = r.Close(runtimeCtx)
		return fmt.Errorf("recognize: instantiate afp.wasm: %w", err)
	}
	if _, err := mod.ExportedFunction("C").Call(engineCtx); err != nil {
		_ = r.Close(runtimeCtx)
		return fmt.Errorf("recognize: run afp.wasm ctors: %w", err)
	}

	s.runtime = r
	s.engine = engine
	s.ctx = engineCtx
	s.started = true
	return nil
}

// Stop closes the WASM runtime.
func (s *RecognizeService) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}
	s.started = false
	if s.runtime != nil {
		err := s.runtime.Close(context.Background())
		s.runtime = nil
		s.engine = nil
		s.ctx = nil
		return err
	}
	return nil
}

// Recognize decodes the audio to PCM with ffmpeg, computes the fingerprint with
// afp.wasm, and matches it against the NetEase audio-match API.
func (s *RecognizeService) Recognize(ctx context.Context, audioData []byte) (*RecognizeResult, error) {
	if len(audioData) == 0 {
		return nil, errors.New("recognize: empty audio data")
	}

	pcm, err := decodePCM(ctx, audioData)
	if err != nil {
		return nil, err
	}
	if len(pcm) < afpMinSamples {
		return nil, fmt.Errorf("recognize: audio too short, need >= %d samples (~10s at 48kHz), got %d", afpMinSamples, len(pcm))
	}

	encoded, err := s.encodeFingerprint(ctx, pcm)
	if err != nil {
		return nil, err
	}

	return s.match(ctx, encoded)
}

// encodeFingerprint builds the fingerprint input and runs afp.wasm's
// ExtractQueryFP, returning the base64-encoded fingerprint. WASM calls are
// serialized because the embind engine holds shared per-module state.
func (s *RecognizeService) encodeFingerprint(ctx context.Context, pcm []float32) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started || s.engine == nil {
		return "", errors.New("recognize: service not started")
	}

	// fp[i] = pcm[i*stride + fromSamples], packed little-endian float32.
	argBytes := make([]byte, afpFingerprintN*4)
	for i := 0; i < afpFingerprintN; i++ {
		v := pcm[i*afpStride+afpFromSamples]
		binary.LittleEndian.PutUint32(argBytes[i*4:], math.Float32bits(v))
	}

	// The emscripten::val argument is consumed via the std::string wire path,
	// so the raw fingerprint bytes are passed as a Go string.
	callCtx := s.engine.Attach(context.WithoutCancel(ctx))
	res, err := s.engine.CallPublicSymbol(callCtx, "ExtractQueryFP", string(argBytes))
	if err != nil {
		return "", fmt.Errorf("recognize: ExtractQueryFP: %w", err)
	}

	out, err := readVectorInt8(callCtx, res)
	if err != nil {
		return "", err
	}
	if len(out) <= 64 {
		return "", fmt.Errorf("recognize: fingerprint too short (%d bytes)", len(out))
	}

	return base64.StdEncoding.EncodeToString(out), nil
}

// match posts the fingerprint to the NetEase audio-match API.
func (s *RecognizeService) match(ctx context.Context, encoded string) (*RecognizeResult, error) {
	form := url.Values{}
	form.Set("sessionId", "441df692-afea-4a54-8aff-f5f20fd34f12")
	form.Set("algorithmCode", "shazam_v2")
	form.Set("duration", strconv.Itoa(afpDurationSec))
	form.Set("rawdata", encoded)
	form.Set("times", "2")
	form.Set("decrypt", "1")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, neteaseMatchURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7")
	req.Header.Set("Origin", recognizeOrigin)
	req.Header.Set("User-Agent", recognizeUA)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("recognize: audio-match request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("recognize: audio-match returned status %d", resp.StatusCode)
	}

	var result RecognizeResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("recognize: parse audio-match response: %w", err)
	}
	return &result, nil
}

// loadAFPWasm returns the fingerprint encoder embedded into the binary.
func loadAFPWasm() ([]byte, error) {
	if len(afpWasm) == 0 {
		return nil, fmt.Errorf("recognize: embedded afp.wasm is empty")
	}
	return afpWasm, nil
}

// decodePCM converts arbitrary audio bytes to mono float32 PCM at 48kHz using
// ffmpeg (reading from stdin, writing f32le to stdout).
func decodePCM(ctx context.Context, audioData []byte) ([]float32, error) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("recognize: ffmpeg not found: %w", err)
	}

	decodeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(decodeCtx, ffmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-i", "pipe:0",
		"-ac", "1",
		"-ar", strconv.Itoa(afpSampleRate),
		"-f", "f32le",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(audioData)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("recognize: ffmpeg decode failed: %w (%s)", err, bytes.TrimSpace(stderr.Bytes()))
	}

	raw := stdout.Bytes()
	n := len(raw) / 4
	pcm := make([]float32, n)
	for i := 0; i < n; i++ {
		pcm[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return pcm, nil
}

// readVectorInt8 reads the std::vector<int8_t> returned by ExtractQueryFP. The
// embind engine returns it as a class instance with size()/get(i) methods; the
// instance is deleted afterwards to free WASM heap memory.
func readVectorInt8(ctx context.Context, res any) ([]byte, error) {
	switch v := res.(type) {
	case []int8:
		out := make([]byte, len(v))
		for i, b := range v {
			out[i] = byte(b)
		}
		return out, nil
	case []byte:
		return v, nil
	case embind.ClassBase:
		return readVectorClass(ctx, v)
	default:
		return nil, fmt.Errorf("recognize: unexpected ExtractQueryFP return type %T", res)
	}
}

func readVectorClass(ctx context.Context, c embind.ClassBase) ([]byte, error) {
	defer func() { _ = c.DeleteInstance(ctx, c) }()

	szVal, err := c.CallInstanceMethod(ctx, c, "size")
	if err != nil {
		return nil, fmt.Errorf("recognize: vector size(): %w", err)
	}
	n, ok := toInt(szVal)
	if !ok {
		return nil, fmt.Errorf("recognize: vector size() returned %T", szVal)
	}

	out := make([]byte, n)
	for i := 0; i < n; i++ {
		val, err := c.CallInstanceMethod(ctx, c, "get", uint32(i))
		if err != nil {
			return nil, fmt.Errorf("recognize: vector get(%d): %w", i, err)
		}
		bv, ok := toInt(val)
		if !ok {
			return nil, fmt.Errorf("recognize: vector get(%d) returned %T", i, val)
		}
		out[i] = byte(bv)
	}
	return out, nil
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case uint:
		return int(n), true
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		return int(n), true
	case uint64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
