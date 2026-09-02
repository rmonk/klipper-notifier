package moonraker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

func TestMoonrakerQueryStatusWithAFC(t *testing.T) {
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
						"toolhead": map[string]interface{}{
							"extruder": "extruder2",
						},
						"AFC": map[string]interface{}{
							"current_lane": "E2",
							"lanes":        []string{"E0", "E1", "E2", "E3"},
						},
						"AFC_lane E0": map[string]interface{}{
							"name":          "E0",
							"material":      "PLA",
							"filament_name": "Generic PLA",
							"color":         "#000000",
							"extruder":      "extruder",
							"status":        "empty",
						},
						"AFC_lane E2": map[string]interface{}{
							"name":          "E2",
							"material":      "PETG",
							"filament_name": "Generic PETG",
							"color":         "#8E24AA",
							"extruder":      "extruder2",
							"status":        "Loaded",
							"tool_loaded":   true,
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
	if st.FilamentType != "PETG" {
		t.Errorf("expected AFC filament type PETG, got %s", st.FilamentType)
	}
	if st.FilamentColor != "#8E24AA" {
		t.Errorf("expected AFC filament color #8E24AA, got %s", st.FilamentColor)
	}
}

func TestMoonrakerQueryStatusWithoutAFCFallbackAndSinglePull(t *testing.T) {
	var metaCalls int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/printer/objects/query" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"status": map[string]interface{}{
						"print_stats": map[string]interface{}{
							"filename":       "benchy_fallback.gcode",
							"state":          "printing",
							"print_duration": 60.0,
							"total_duration": 80.0,
						},
						"display_status": map[string]interface{}{
							"progress": 0.20,
						},
						"heater_bed": map[string]interface{}{
							"temperature": 60.0,
						},
						"extruder": map[string]interface{}{
							"temperature": 215.0,
						},
					},
				},
			})
			return
		}
		if r.URL.Path == "/server/files/metadata" {
			atomic.AddInt32(&metaCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"estimated_time":  600.0,
					"layer_count":     250,
					"filament_type":   "PLA",
					"filament_colour": "#1E88E5",
					"filament_name":   "Polymaker PolyLite PLA",
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

	ctx := context.Background()

	// 1st query -> fetches metadata from gcode
	st1, err := client.QueryStatus(ctx)
	if err != nil {
		t.Fatalf("1st QueryStatus failed: %v", err)
	}
	if st1.FilamentType != "PLA" {
		t.Errorf("expected gcode fallback filament PLA, got %s", st1.FilamentType)
	}
	if st1.FilamentColor != "#1E88E5" {
		t.Errorf("expected gcode fallback color #1E88E5, got %s", st1.FilamentColor)
	}
	if atomic.LoadInt32(&metaCalls) != 1 {
		t.Errorf("expected 1 metadata call, got %d", metaCalls)
	}

	// 2nd query (subsequent poll on same print) -> uses cached metadata, does NOT hit /server/files/metadata again
	st2, err := client.QueryStatus(ctx)
	if err != nil {
		t.Fatalf("2nd QueryStatus failed: %v", err)
	}
	if st2.FilamentType != "PLA" {
		t.Errorf("expected cached filament PLA, got %s", st2.FilamentType)
	}
	if atomic.LoadInt32(&metaCalls) != 1 {
		t.Errorf("expected metadata to only be pulled once, but metaCalls = %d", metaCalls)
	}
}

func TestMoonrakerFilenameChangeResetsFilament(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:7125", "")
	if err != nil {
		t.Fatalf("client init error: %v", err)
	}

	client.status.Filename = "fileA.gcode"
	client.status.FilamentType = "PLA"
	client.status.FilamentColor = "#1E88E5"
	client.status.FilamentName = "Generic Blue PLA"

	// New filename arrived in print_stats
	rawStats := map[string]json.RawMessage{
		"print_stats": json.RawMessage(`{"filename":"fileB.gcode","state":"printing"}`),
	}
	client.updateFromRawStatus(rawStats)

	if client.status.Filename != "fileB.gcode" {
		t.Errorf("expected filename fileB.gcode, got %s", client.status.Filename)
	}
	if client.status.FilamentType != "" || client.status.FilamentColor != "" {
		t.Errorf("expected filament reset on new filename, got type=%q color=%q", client.status.FilamentType, client.status.FilamentColor)
	}
}

func TestGCodeMetadataFilamentParsing(t *testing.T) {
	meta := &GCodeMetadata{
		FilamentType:   "PLA;TPU;PETG;PETG",
		FilamentName:   `Generic PLA @System";"Generic TPU @System";"Bambu PETG HF @System";"Bambu PETG HF @System`,
		FilamentColour: "#080A0D;#000000;#1E88E5;#8E24AA",
		FilamentWeight: []interface{}{0.0, 0.0, 14.13, 0.0},
	}

	fType, fColor, fName := meta.GetFilamentInfo()
	if fType != "PETG" {
		t.Errorf("expected primary filament PETG, got %s", fType)
	}
	if fColor != "#1E88E5" {
		t.Errorf("expected primary color #1E88E5, got %s", fColor)
	}
	if fName != "Bambu PETG HF @System" {
		t.Errorf("expected Bambu PETG HF @System, got %s", fName)
	}
}

func TestGCodeMetadataFilamentParsingWithEmptySlots(t *testing.T) {
	meta := &GCodeMetadata{
		FilamentType:   ";;PETG;PLA",
		FilamentColour: ";;#8E24AA;#1E88E5",
		FilamentWeight: ";;25.5;5.0",
	}

	fType, fColor, _ := meta.GetFilamentInfo()
	if fType != "PETG + PLA" {
		t.Errorf("expected 'PETG + PLA', got %s", fType)
	}
	if fColor != "#8E24AA" {
		t.Errorf("expected primary color #8E24AA (highest weight), got %s", fColor)
	}
}
