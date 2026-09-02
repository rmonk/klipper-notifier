package moonraker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMoonrakerCheckConnection(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/server/info" {
			if r.Header.Get("X-Api-Key") != "my-secret-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
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
	defer ts.Close()

	// 1. With wrong key -> should fail
	c1, err := NewClient(ts.URL, "wrong-key")
	if err != nil {
		t.Fatalf("client init error: %v", err)
	}
	if _, err := c1.CheckConnection(context.Background()); err == nil {
		t.Fatal("expected unauthorized error, got nil")
	}

	// 2. With correct key -> should succeed
	c2, err := NewClient(ts.URL, "my-secret-key")
	if err != nil {
		t.Fatalf("client init error: %v", err)
	}
	info, err := c2.CheckConnection(context.Background())
	if err != nil {
		t.Fatalf("unexpected connection error: %v", err)
	}
	if !info.KlippyConnected || info.KlippyState != "ready" {
		t.Errorf("unexpected server info: %+v", info)
	}
}

func TestMoonrakerQueryStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/printer/objects/query" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"status": map[string]interface{}{
						"print_stats": map[string]interface{}{
							"filename":       "voron_cube.gcode",
							"state":          "printing",
							"print_duration": 120.0,
							"total_duration": 150.0,
						},
						"display_status": map[string]interface{}{
							"progress": 0.45,
							"message":  "Printing layer 10",
						},
						"heater_bed": map[string]interface{}{
							"temperature": 60.2,
							"target":      60.0,
						},
						"extruder": map[string]interface{}{
							"temperature": 210.5,
							"target":      210.0,
						},
					},
				},
			})
			return
		}
		if r.URL.Path == "/server/files/metadata" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"estimated_time": 300.0,
					"layer_count":    100,
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	client, err := NewClient(ts.URL, "")
	if err != nil {
		t.Fatalf("client init error: %v", err)
	}

	st, err := client.QueryStatus(context.Background())
	if err != nil {
		t.Fatalf("QueryStatus failed: %v", err)
	}

	if st.PrintState != "printing" {
		t.Errorf("expected printing state, got %s", st.PrintState)
	}
	if st.Filename != "voron_cube.gcode" {
		t.Errorf("expected voron_cube.gcode, got %s", st.Filename)
	}
	if st.Progress != 0.45 {
		t.Errorf("expected progress 0.45, got %f", st.Progress)
	}
	if st.ExtruderTemp < 210.0 || st.BedTemp < 60.0 {
		t.Errorf("unexpected temperatures: extruder=%f bed=%f", st.ExtruderTemp, st.BedTemp)
	}
	if st.TotalLayers != 100 {
		t.Errorf("expected 100 total layers from metadata, got %d", st.TotalLayers)
	}
}
