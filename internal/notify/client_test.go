package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFormatTint(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"teal", "#00A76F"},
		{"green", "#34C759"},
		{"orange", "#FF9500"},
		{"red", "#FF3B30"},
		{"blue", "#007AFF"},
		{"purple", "#AF52DE"},
		{"#8E24AA", "#8E24AA"},
		{"8E24AA", "#8E24AA"},
		{"#1E88E5", "#1E88E5"},
		{"#1E88E580", "#1E88E580"},
		// Black & near-black contrast remapping
		{"black", "#8E8E93"},
		{"#000000", "#8E8E93"},
		{"#00000080", "#8E8E9380"},
		{"000000", "#8E8E93"},
		{"#080A0D", "#8E8E93"},
		{"#1A1A1A", "#8E8E93"},
		// White & near-white contrast remapping
		{"white", "#D1D1D6"},
		{"#FFFFFF", "#D1D1D6"},
		{"#FFFFFF80", "#D1D1D680"},
		{"FFFFFF", "#D1D1D6"},
		{"#FAFAFA", "#D1D1D6"},
		{"#F0F0F0", "#D1D1D6"},
		{"", ""},
	}

	for _, tt := range tests {
		got := FormatTint(tt.input)
		if got != tt.expected {
			t.Errorf("FormatTint(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestNotifyClientLinkSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/link" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("id") != "dev1" || r.URL.Query().Get("token") != "tok1" {
			t.Errorf("unexpected query params: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"type":    "device",
			"name":    "My iPhone",
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "dev1", "tok1", "", false)
	resp, err := client.Link(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success || resp.Type != "device" {
		t.Errorf("unexpected link response: %+v", resp)
	}
}

func TestNotifyClientLinkGroupRejection(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"type":    "group",
			"name":    "My Group",
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "grp1", "tok1", "", false)
	_, err := client.Link(context.Background())
	if err == nil {
		t.Fatal("expected error on group credential, got nil")
	}
}

func TestNotifyClientLiveActivityLifecycleAndTint(t *testing.T) {
	var startCalled, updateCalled, endCalled bool
	var startTint, updateTint, endTint string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		bodyBytes, _ := io.ReadAll(r.Body)

		var payload map[string]interface{}
		_ = json.Unmarshal(bodyBytes, &payload)

		switch r.Method {
		case http.MethodPost:
			if r.URL.Path == "/live-activity/dev1" && r.URL.Query().Get("new") == "1" {
				startCalled = true
				if tVal, ok := payload["tint"].(string); ok {
					startTint = tVal
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success":    true,
					"activityId": "act-12345",
				})
				return
			}
			if r.URL.Path == "/live-activity/act-12345" {
				updateCalled = true
				if tVal, ok := payload["tint"].(string); ok {
					updateTint = tVal
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
				})
				return
			}
		case http.MethodDelete:
			if r.URL.Path == "/live-activity/act-12345" {
				endCalled = true
				if tVal, ok := payload["tint"].(string); ok {
					endTint = tVal
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
				})
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "dev1", "tok1", "", false)
	ctx := context.Background()

	// 1. Start with color "#8E24AA"
	started, err := client.Start(ctx, TileContent{
		Title:  "Voron 2.4",
		Status: "Printing",
		Color:  "#8E24AA",
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if started.ActivityID != "act-12345" {
		t.Errorf("expected act-12345, got %s", started.ActivityID)
	}
	if startTint != "#8E24AA" {
		t.Errorf("expected start tint #8E24AA, got %q", startTint)
	}

	// 2. Update with color "blue"
	prog := 50
	err = client.Update(ctx, started.ActivityID, TileContent{
		Progress: &prog,
		Status:   "Printing",
		Color:    "blue",
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updateTint != "#007AFF" {
		t.Errorf("expected update tint #007AFF, got %q", updateTint)
	}

	// 3. End with green
	endProg := 100
	endTile := TileContent{
		Progress: &endProg,
		Status:   "Done",
		Color:    "green",
	}
	err = client.End(ctx, started.ActivityID, &endTile, 300)
	if err != nil {
		t.Fatalf("end failed: %v", err)
	}
	if endTint != "#34C759" {
		t.Errorf("expected end tint #34C759, got %q", endTint)
	}

	if !startCalled || !updateCalled || !endCalled {
		t.Errorf("lifecycle calls missing: start=%v update=%v end=%v", startCalled, updateCalled, endCalled)
	}
}
