package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultMoonrakerURL  = "http://127.0.0.1:7125"
	DefaultNotifyBaseURL = "https://push.getnotifyapp.com"
	DefaultNotifyIconURL = "https://icons.getnotifyapp.com/icons/mt39aefs-tjpz3wae.png"
	DefaultPrinterName   = "Klipper"
	DefaultPollInterval  = 2 * time.Second
	DefaultKeepForSec    = 300
)

type Config struct {
	MoonrakerURL            string        `yaml:"moonraker_url"`
	MoonrakerAPIKey         string        `yaml:"moonraker_api_key"`
	NotifyDeviceID          string        `yaml:"notify_device_id"`
	NotifyDeviceToken       string        `yaml:"notify_device_token"`
	NotifyBaseURL           string        `yaml:"notify_base_url"`
	NotifyIconURL           string        `yaml:"notify_icon_url"`
	PrinterName             string        `yaml:"printer_name"`
	PollInterval            time.Duration `yaml:"poll_interval"`
	KeepForSeconds          int           `yaml:"keep_for_seconds"`
	ShowMetrics             bool          `yaml:"show_metrics"`
	ShowTrailingLayer       bool          `yaml:"show_trailing_layer"`
	EnablePushNotifications bool          `yaml:"enable_push_notifications"`
	TestMode                bool          `yaml:"test_mode"`
	DryRun                  bool          `yaml:"dry_run"`
	Verbose                 bool          `yaml:"verbose"`
}

// NewDefault returns a Config populated with sensible defaults.
func NewDefault() *Config {
	return &Config{
		MoonrakerURL:            DefaultMoonrakerURL,
		NotifyBaseURL:           DefaultNotifyBaseURL,
		NotifyIconURL:           DefaultNotifyIconURL,
		PrinterName:             DefaultPrinterName,
		PollInterval:            DefaultPollInterval,
		KeepForSeconds:          DefaultKeepForSec,
		ShowMetrics:             false,
		ShowTrailingLayer:       false,
		EnablePushNotifications: false,
	}
}

// Load reads configuration from a config file (if specified or present) and merges with environment variables.
func Load(configPath string) (*Config, error) {
	cfg := NewDefault()

	// 1. If a config file is provided, load it
	if configPath != "" {
		if err := cfg.loadFile(configPath); err != nil {
			return nil, fmt.Errorf("failed to load config file %s: %w", configPath, err)
		}
	} else if fileExists("notify-klipper.yaml") {
		_ = cfg.loadFile("notify-klipper.yaml")
	} else if fileExists("notify-klipper.yml") {
		_ = cfg.loadFile("notify-klipper.yml")
	} else if fileExists(".env") {
		_ = loadDotEnv(".env")
	} else if fileExists("../.env") {
		_ = loadDotEnv("../.env")
	}

	// 2. Override from environment variables
	if err := cfg.loadFromEnv(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) loadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// If the file looks like YAML
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		return yaml.Unmarshal(data, c)
	}

	// Otherwise treat as env file
	return parseEnvBytes(data)
}

func (c *Config) loadFromEnv() error {
	if val := getEnvSecret("MOONRAKER_URL"); val != "" {
		c.MoonrakerURL = val
	}
	if val := getEnvSecret("MOONRAKER_API_KEY"); val != "" {
		c.MoonrakerAPIKey = val
	}
	if val := getEnvSecret("NOTIFY_DEVICE_ID"); val != "" {
		c.NotifyDeviceID = val
	}
	if val := getEnvSecret("NOTIFY_DEVICE_TOKEN"); val != "" {
		c.NotifyDeviceToken = val
	}
	if val := getEnvSecret("NOTIFY_BASE_URL"); val != "" {
		c.NotifyBaseURL = val
	}
	if val := getEnvSecret("NOTIFY_ICON_URL"); val != "" {
		c.NotifyIconURL = val
	}
	if val := getEnvSecret("PRINTER_NAME"); val != "" {
		c.PrinterName = val
	}
	if val := os.Getenv("POLL_INTERVAL"); val != "" {
		if dur, err := time.ParseDuration(val); err == nil {
			c.PollInterval = dur
		} else if sec, err := strconv.Atoi(val); err == nil {
			c.PollInterval = time.Duration(sec) * time.Second
		}
	}
	if val := os.Getenv("KEEP_FOR_SECONDS"); val != "" {
		if sec, err := strconv.Atoi(val); err == nil {
			c.KeepForSeconds = sec
		}
	}
	if val := os.Getenv("SHOW_METRICS"); val != "" {
		c.ShowMetrics = parseBool(val)
	}
	if val := os.Getenv("SHOW_TRAILING_LAYER"); val != "" {
		c.ShowTrailingLayer = parseBool(val)
	}
	if val := os.Getenv("ENABLE_PUSH_NOTIFICATIONS"); val != "" {
		c.EnablePushNotifications = parseBool(val)
	}
	if val := os.Getenv("DRY_RUN"); val != "" {
		c.DryRun = parseBool(val)
	}
	if val := os.Getenv("VERBOSE"); val != "" {
		c.Verbose = parseBool(val)
	}

	return nil
}

func (c *Config) Validate(requireNotify bool) error {
	if c.MoonrakerURL == "" {
		return errors.New("moonraker_url cannot be empty")
	}
	u, err := url.Parse(c.MoonrakerURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "ws" && u.Scheme != "wss") {
		return fmt.Errorf("invalid moonraker_url: %q (must start with http://, https://, ws://, or wss://)", c.MoonrakerURL)
	}

	// Normalize Moonraker URL to http/https (strip trailing slash)
	c.MoonrakerURL = strings.TrimRight(c.MoonrakerURL, "/")

	if requireNotify {
		if c.NotifyDeviceID == "" {
			return errors.New("notify_device_id is required (set NOTIFY_DEVICE_ID or NOTIFY_DEVICE_ID_FILE)")
		}
		if c.NotifyDeviceToken == "" {
			return errors.New("notify_device_token is required (set NOTIFY_DEVICE_TOKEN or NOTIFY_DEVICE_TOKEN_FILE)")
		}
	}

	if c.NotifyBaseURL == "" {
		c.NotifyBaseURL = DefaultNotifyBaseURL
	}
	c.NotifyBaseURL = strings.TrimRight(c.NotifyBaseURL, "/")

	if c.NotifyIconURL == "" {
		c.NotifyIconURL = DefaultNotifyIconURL
	}

	if c.PrinterName == "" {
		c.PrinterName = DefaultPrinterName
	}

	if c.PollInterval <= 0 {
		c.PollInterval = DefaultPollInterval
	}

	return nil
}

// getEnvSecret reads the environment variable key or key_FILE if key_FILE is set.
func getEnvSecret(key string) string {
	fileKey := key + "_FILE"
	if filePath := os.Getenv(fileKey); filePath != "" {
		if content, err := os.ReadFile(filePath); err == nil {
			return strings.TrimSpace(string(content))
		}
	}
	return strings.TrimSpace(os.Getenv(key))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func parseBool(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return parseEnvBytes(data)
}

func parseEnvBytes(data []byte) error {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			// Strip surrounding quotes
			v = strings.Trim(v, `"'`)
			if os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
	}
	return scanner.Err()
}
