package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"rexion/internal/config"
)

var (
	voiceWhisperPath     string
	voiceInputEnabled    bool
	voiceWhisperModel    string
	voiceWhisperLanguage string
)

func initVoiceInput() {
	cfg, _ := config.Load()
	voiceInputEnabled = cfg.VoiceInput.EnabledOrDefault()
	if !voiceInputEnabled {
		return
	}
	voiceWhisperPath = resolveWhisperPath(cfg.VoiceInput.WhisperPath)
	voiceWhisperModel = cfg.VoiceInput.ModelOrDefault()
	voiceWhisperLanguage = cfg.VoiceInput.LanguageOrDefault()
}

func resolveWhisperPath(configPath string) string {
	if configPath != "" {
		return configPath
	}
	for _, name := range []string{"whisper", "whisper-cli", "whisper.cpp"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func IsVoiceInputAvailable() bool {
	return voiceInputEnabled && voiceWhisperPath != ""
}

func (a *App) VoiceInputAvailable() bool {
	return IsVoiceInputAvailable()
}

func (a *App) TranscribeAudio(wavBytes []byte) (string, error) {
	if !IsVoiceInputAvailable() {
		return "", fmt.Errorf("voice input not available: whisper not found")
	}

	tmpDir, err := os.MkdirTemp("", "Rexion-voice")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, "input.wav")
	if err := os.WriteFile(tmpFile, wavBytes, 0600); err != nil {
		return "", fmt.Errorf("write temp wav: %w", err)
	}

	// Model and language come from the resolved voice_input config
	// (voice.model / voice.language), overridable live via SetVoiceModel /
	// SetVoiceLanguage. Defaults: model=base, language=auto.
	args := []string{"--model", voiceWhisperModel, "--language", voiceWhisperLanguage, "--output-format", "txt", tmpFile}
	cmd := exec.Command(voiceWhisperPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("whisper failed: %w, stderr: %s", err, stderr.String())
	}

	txtFile := strings.TrimSuffix(tmpFile, ".wav") + ".txt"
	result, err := os.ReadFile(txtFile)
	if err != nil {
		return strings.TrimSpace(stdout.String()), nil
	}
	return strings.TrimSpace(string(result)), nil
}

// SetVoiceModel updates the whisper model used for transcription (tiny|base|
// small|medium|large). It persists the choice to the user config (so it survives
// restarts) and updates the live package var so the next TranscribeAudio call
// uses it without a restart. An empty value resets to the default "base".
// applyConfigOnly is used (not applyConfigChange) because voice params don't
// affect the agent controller and need no rebuild.
func (a *App) SetVoiceModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		model = "base"
	}
	if err := a.applyConfigOnly(func(c *config.Config) error {
		c.VoiceInput.Model = model
		return nil
	}); err != nil {
		return err
	}
	voiceWhisperModel = model
	return nil
}

// SetVoiceLanguage updates the spoken language passed to whisper (auto|zh|en|ja|
// ...). Persisted to the user config and applied live. An empty value resets to
// the default "auto" (let whisper auto-detect).
func (a *App) SetVoiceLanguage(language string) error {
	language = strings.TrimSpace(language)
	if language == "" {
		language = "auto"
	}
	if err := a.applyConfigOnly(func(c *config.Config) error {
		c.VoiceInput.Language = language
		return nil
	}); err != nil {
		return err
	}
	voiceWhisperLanguage = language
	return nil
}
