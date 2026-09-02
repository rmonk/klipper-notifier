package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func TestNotifyClientLiveActivityLifecycle(t *testing.T) {
	var startCalled, updateCalled, endCalled bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			if r.URL.Path == "/live-activity/dev1" && r.URL.Query().Get("new") == "1" {
				startCalled = true
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success":    true,
					"activityId": "act-12345",
				})
				return
			}
			if r.URL.Path == "/live-activity/act-12345" {
				updateCalled = true
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": true,
				})
				return
			}
		case http.MethodDelete:
			if r.URL.Path == "/live-activity/act-12345" {
				endCalled = true
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

	// 1. Start
	started, err := client.Start(ctx, TileContent{
		Title:  "Voron 2.4",
		Status: "Printing",
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if started.ActivityID != "act-12345" {
		t.Errorf("expected act-12345, got %s", started.ActivityID)
	}

	// 2. Update
	prog := 50
	err = client.Update(ctx, started.ActivityID, TileContent{
		Progress: &prog,
		Status:   "Printing",
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	// 3. End
	err = client.End(ctx, started.ActivityID, nil, 300)
	if err != nil {
		t.Fatalf("end failed: %v", err)
	}

	if !startCalled || !updateCalled || !endCalled {
		t.Errorf("lifecycle calls missing: start=%v update=%v end=%v", startCalled, updateCalled, endCalled)
	}
}
