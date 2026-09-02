package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	cfg := NewDefault()
	if cfg.MoonrakerURL != DefaultMoonrakerURL {
		t.Errorf("expected default moonraker url %s, got %s", DefaultMoonrakerURL, cfg.MoonrakerURL)
	}
	if cfg.NotifyBaseURL != DefaultNotifyBaseURL {
		t.Errorf("expected default notify base url %s, got %s", DefaultNotifyBaseURL, cfg.NotifyBaseURL)
	}
	if cfg.PrinterName != DefaultPrinterName {
		t.Errorf("expected default printer name %s, got %s", DefaultPrinterName, cfg.PrinterName)
	}
}

func TestConfigLoadEnv(t *testing.T) {
	os.Setenv("MOONRAKER_URL", "http://192.168.1.50:7125")
	os.Setenv("NOTIFY_DEVICE_ID", "device-123")
	os.Setenv("NOTIFY_DEVICE_TOKEN", "token-abc")
	os.Setenv("PRINTER_NAME", "Voron 2.4")
	os.Setenv("POLL_INTERVAL", "5s")
	os.Setenv("DRY_RUN", "true")
	os.Setenv("SHOW_METRICS", "true")
	os.Setenv("SHOW_TRAILING_LAYER", "true")
	os.Setenv("ENABLE_PUSH_NOTIFICATIONS", "true")
	defer func() {
		os.Unsetenv("MOONRAKER_URL")
		os.Unsetenv("NOTIFY_DEVICE_ID")
		os.Unsetenv("NOTIFY_DEVICE_TOKEN")
		os.Unsetenv("PRINTER_NAME")
		os.Unsetenv("POLL_INTERVAL")
		os.Unsetenv("DRY_RUN")
		os.Unsetenv("SHOW_METRICS")
		os.Unsetenv("SHOW_TRAILING_LAYER")
		os.Unsetenv("ENABLE_PUSH_NOTIFICATIONS")
	}()

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if cfg.MoonrakerURL != "http://192.168.1.50:7125" {
		t.Errorf("expected moonraker url http://192.168.1.50:7125, got %s", cfg.MoonrakerURL)
	}
	if cfg.NotifyDeviceID != "device-123" {
		t.Errorf("expected notify device id device-123, got %s", cfg.NotifyDeviceID)
	}
	if cfg.NotifyDeviceToken != "token-abc" {
		t.Errorf("expected notify device token token-abc, got %s", cfg.NotifyDeviceToken)
	}
	if cfg.PrinterName != "Voron 2.4" {
		t.Errorf("expected printer name Voron 2.4, got %s", cfg.PrinterName)
	}
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("expected poll interval 5s, got %v", cfg.PollInterval)
	}
	if !cfg.DryRun {
		t.Errorf("expected dry run to be true")
	}
	if !cfg.ShowMetrics {
		t.Errorf("expected show metrics to be true")
	}
	if !cfg.ShowTrailingLayer {
		t.Errorf("expected show trailing layer to be true")
	}
	if !cfg.EnablePushNotifications {
		t.Errorf("expected enable push notifications to be true")
	}
}

func TestConfigSecretFile(t *testing.T) {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "secret_token.txt")
	if err := os.WriteFile(tokenFile, []byte("super-secret-token\n"), 0600); err != nil {
		t.Fatalf("failed to write secret file: %v", err)
	}

	os.Setenv("NOTIFY_DEVICE_TOKEN_FILE", tokenFile)
	defer os.Unsetenv("NOTIFY_DEVICE_TOKEN_FILE")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if cfg.NotifyDeviceToken != "super-secret-token" {
		t.Errorf("expected token from file 'super-secret-token', got '%s'", cfg.NotifyDeviceToken)
	}
}

func TestConfigValidation(t *testing.T) {
	cfg := NewDefault()
	cfg.MoonrakerURL = "invalid-url"
	if err := cfg.Validate(false); err == nil {
		t.Errorf("expected error on invalid url, got nil")
	}

	cfg.MoonrakerURL = "http://127.0.0.1:7125"
	if err := cfg.Validate(true); err == nil {
		t.Errorf("expected error when notify credentials missing and required, got nil")
	}

	cfg.NotifyDeviceID = "dev1"
	cfg.NotifyDeviceToken = "tok1"
	if err := cfg.Validate(true); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}
