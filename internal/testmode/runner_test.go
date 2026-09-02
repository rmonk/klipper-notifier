package testmode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/printer-notifier/notify-klipper/internal/config"
	"github.com/printer-notifier/notify-klipper/internal/moonraker"
	"github.com/printer-notifier/notify-klipper/internal/notify"
)

func TestTestModeRunnerFast(t *testing.T) {
	var startCalls, updateCalls, endCalls, notifyCalls int32

	notifyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/link" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"type":    "device",
				"name":    "iPhone 16 Pro",
			})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/live-activity/dev1" {
			atomic.AddInt32(&startCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success":    true,
				"activityId": "test-act-1",
			})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/live-activity/test-act-1" {
			atomic.AddInt32(&updateCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/live-activity/test-act-1" {
			atomic.AddInt32(&endCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/notify-json/dev1" {
			atomic.AddInt32(&notifyCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
			return
		}
		http.NotFound(w, r)
	}))
	defer notifyServer.Close()

	moonServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/server/info" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"klippy_connected":  true,
					"klippy_state":      "ready",
					"moonraker_version": "v0.8.0",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer moonServer.Close()

	cfg := config.NewDefault()
	cfg.MoonrakerURL = moonServer.URL
	cfg.NotifyDeviceID = "dev1"
	cfg.NotifyDeviceToken = "tok1"
	cfg.NotifyBaseURL = notifyServer.URL

	moonClient, err := moonraker.NewClient(cfg.MoonrakerURL, "")
	if err != nil {
		t.Fatalf("failed to create moonraker client: %v", err)
	}

	notifClient := notify.NewClient(cfg.NotifyBaseURL, cfg.NotifyDeviceID, cfg.NotifyDeviceToken, "", false)

	runner := NewRunner(cfg, moonClient, notifClient)
	runner.SetInterval(10 * time.Millisecond) // fast execution for unit test

	err = runner.Run(context.Background())
	if err != nil {
		t.Fatalf("TestMode runner failed: %v", err)
	}

	if atomic.LoadInt32(&startCalls) != 1 {
		t.Errorf("expected 1 start call, got %d", startCalls)
	}
	if atomic.LoadInt32(&updateCalls) != 3 {
		t.Errorf("expected 3 update calls (25%%, 50%%, 75%%), got %d", updateCalls)
	}
	if atomic.LoadInt32(&endCalls) != 1 {
		t.Errorf("expected 1 end call, got %d", endCalls)
	}
	if atomic.LoadInt32(&notifyCalls) != 0 {
		t.Errorf("expected 0 push notifications when disabled, got %d", notifyCalls)
	}

	// Test with push notifications enabled
	cfg.EnablePushNotifications = true
	atomic.StoreInt32(&startCalls, 0)
	atomic.StoreInt32(&updateCalls, 0)
	atomic.StoreInt32(&endCalls, 0)
	atomic.StoreInt32(&notifyCalls, 0)

	err = runner.Run(context.Background())
	if err != nil {
		t.Fatalf("TestMode runner failed with push notifications: %v", err)
	}
	if atomic.LoadInt32(&notifyCalls) != 2 {
		t.Errorf("expected 2 push notifications when enabled, got %d", notifyCalls)
	}
}
