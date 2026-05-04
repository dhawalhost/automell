package messaging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhawalhost/automell/config"
)

// Transcriber converts raw audio bytes to a text transcription.
type Transcriber interface {
	Transcribe(ctx context.Context, audioData []byte, mimeType string) (string, error)
}

// NIMTranscriber sends audio to the NVIDIA NIM ASR endpoint.
type NIMTranscriber struct {
	apiKey     string
	httpClient *http.Client
}

// LocalWhisperTranscriber runs the `whisper` CLI as a subprocess.
// Requires the openai-whisper package to be installed and `whisper` in PATH.
type LocalWhisperTranscriber struct {
	model string // e.g. "base", "small", "medium", "large"
}

// NewTranscriber returns the appropriate Transcriber based on config.
// Returns (nil, nil) if voice transcription is not enabled.
func NewTranscriber(cfg *config.Config) (Transcriber, error) {
	if !cfg.VoiceNoteEnabled {
		return nil, nil
	}
	switch strings.ToLower(cfg.WhisperDevice) {
	case "nvidia_nim":
		if cfg.NvidiaNimAPIKey == "" {
			return nil, fmt.Errorf("WHISPER_DEVICE=nvidia_nim requires NVIDIA_NIM_API_KEY to be set")
		}
		return &NIMTranscriber{
			apiKey:     cfg.NvidiaNimAPIKey,
			httpClient: &http.Client{Timeout: 60 * time.Second},
		}, nil
	case "cpu", "cuda", "":
		return &LocalWhisperTranscriber{model: cfg.WhisperModel}, nil
	default:
		return nil, fmt.Errorf("unknown WHISPER_DEVICE=%q; use cpu, cuda, or nvidia_nim", cfg.WhisperDevice)
	}
}

// Transcribe sends audio to the NVIDIA NIM ASR API and returns text.
func (t *NIMTranscriber) Transcribe(ctx context.Context, audioData []byte, mimeType string) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// Determine filename extension from MIME type
	ext := mimeTypeToExt(mimeType)
	fw, err := w.CreateFormFile("file", "audio"+ext)
	if err != nil {
		return "", fmt.Errorf("nim transcribe: create form file: %w", err)
	}
	if _, err := fw.Write(audioData); err != nil {
		return "", fmt.Errorf("nim transcribe: write audio data: %w", err)
	}
	if err := w.WriteField("model", "nvidia/canary-1b"); err != nil {
		return "", fmt.Errorf("nim transcribe: write model field: %w", err)
	}
	w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://integrate.api.nvidia.com/v1/audio/transcriptions",
		&buf)
	if err != nil {
		return "", fmt.Errorf("nim transcribe: create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+t.apiKey)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("nim transcribe: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("nim transcribe: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nim transcribe: status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("nim transcribe: parse response: %w", err)
	}
	return strings.TrimSpace(result.Text), nil
}

// Transcribe writes audio to a temp file, runs the whisper CLI, and returns text.
func (t *LocalWhisperTranscriber) Transcribe(ctx context.Context, audioData []byte, mimeType string) (string, error) {
	ext := mimeTypeToExt(mimeType)
	tmp, err := os.CreateTemp("", "automell-voice-*"+ext)
	if err != nil {
		return "", fmt.Errorf("whisper: create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(audioData); err != nil {
		tmp.Close()
		return "", fmt.Errorf("whisper: write audio: %w", err)
	}
	tmp.Close()

	outDir := os.TempDir()
	args := []string{
		tmp.Name(),
		"--model", t.model,
		"--output_format", "txt",
		"--output_dir", outDir,
	}
	cmd := exec.CommandContext(ctx, "whisper", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("whisper CLI failed: %w — output: %s", err, string(output))
	}

	// whisper writes <input_basename>.txt in outDir
	base := strings.TrimSuffix(filepath.Base(tmp.Name()), ext)
	txtPath := filepath.Join(outDir, base+".txt")
	defer os.Remove(txtPath)

	text, err := os.ReadFile(txtPath)
	if err != nil {
		return "", fmt.Errorf("whisper: read output %s: %w", txtPath, err)
	}
	return strings.TrimSpace(string(text)), nil
}

// mimeTypeToExt maps common audio MIME types to file extensions.
func mimeTypeToExt(mimeType string) string {
	switch {
	case strings.Contains(mimeType, "ogg"):
		return ".ogg"
	case strings.Contains(mimeType, "mp4") || strings.Contains(mimeType, "m4a"):
		return ".m4a"
	case strings.Contains(mimeType, "mpeg") || strings.Contains(mimeType, "mp3"):
		return ".mp3"
	case strings.Contains(mimeType, "wav"):
		return ".wav"
	case strings.Contains(mimeType, "flac"):
		return ".flac"
	case strings.Contains(mimeType, "webm"):
		return ".webm"
	default:
		return ".ogg" // Discord voice messages are typically OGG/Opus
	}
}
