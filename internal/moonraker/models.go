package moonraker

import (
	"strconv"
	"strings"
)

// PrinterInfo represents response from /printer/info
type PrinterInfo struct {
	State        string `json:"state"` // "ready", "startup", "shutdown", "error"
	StateMessage string `json:"state_message"`
	Hostname     string `json:"hostname"`
	KlipperPath  string `json:"klipper_path"`
}

// ServerInfo represents response from /server/info
type ServerInfo struct {
	KlippyConnected bool   `json:"klippy_connected"`
	KlippyState     string `json:"klippy_state"`
	Version         string `json:"moonraker_version"`
}

// PrintStatsInfo contains layer info if available in print_stats.info
type PrintStatsInfo struct {
	CurrentLayer *int `json:"current_layer,omitempty"`
	TotalLayer   *int `json:"total_layer,omitempty"`
}

// PrintStats represents the print_stats object in Moonraker
type PrintStats struct {
	Filename      string         `json:"filename"`
	TotalDuration float64        `json:"total_duration"`
	PrintDuration float64        `json:"print_duration"`
	FilamentUsed  float64        `json:"filament_used"`
	State         string         `json:"state"` // "standby", "printing", "paused", "complete", "error", "cancelled"
	Message       string         `json:"message"`
	Info          PrintStatsInfo `json:"info"`
}

// DisplayStatus represents the display_status object in Moonraker
type DisplayStatus struct {
	Progress float64 `json:"progress"` // 0.0 to 1.0
	Message  string  `json:"message"`
}

// VirtualSDCard represents the virtual_sdcard object in Moonraker
type VirtualSDCard struct {
	Progress     float64 `json:"progress"` // 0.0 to 1.0
	IsActive     bool    `json:"is_active"`
	FilePosition int64   `json:"file_position"`
}

// HeaterBed represents heater_bed object
type HeaterBed struct {
	Temperature float64 `json:"temperature"`
	Target      float64 `json:"target"`
}

// Extruder represents extruder object
type Extruder struct {
	Temperature float64 `json:"temperature"`
	Target      float64 `json:"target"`
}

// Toolhead represents toolhead object
type Toolhead struct {
	Position []float64 `json:"position"`
	Extruder string    `json:"extruder"`
}

// AFCLane represents an individual lane object in AFC (e.g. AFC_lane E0)
type AFCLane struct {
	Name         string `json:"name"`
	Material     string `json:"material"`
	FilamentName string `json:"filament_name"`
	Color        string `json:"color"`
	Extruder     string `json:"extruder"`
	Status       string `json:"status"`
}

// AFCObject represents the root AFC object in Moonraker
type AFCObject struct {
	CurrentLane string   `json:"current_lane"`
	CurrentLoad string   `json:"current_load"`
	Lanes       []string `json:"lanes"`
}

// GCodeMetadata represents metadata returned by /server/files/metadata
type GCodeMetadata struct {
	EstimatedTime       float64     `json:"estimated_time"`
	LayerHeight         float64     `json:"layer_height"`
	FirstLayerHeight    float64     `json:"first_layer_height"`
	LayerCount          int         `json:"layer_count"`
	ObjectHeight        float64     `json:"object_height"`
	FilamentTotal       float64     `json:"filament_total"`
	FilamentType        interface{} `json:"filament_type"`
	FilamentName        interface{} `json:"filament_name"`
	FilamentColour      interface{} `json:"filament_colour"`
	FilamentWeight      interface{} `json:"filament_weight"`
	FilamentWeightTotal float64     `json:"filament_weight_total"`
}

// GetFilamentInfo parses filament metadata and extracts the primary filament type, color, and name.
func (m *GCodeMetadata) GetFilamentInfo() (filType string, filColor string, filName string) {
	types := parseStringOrSlice(m.FilamentType)
	colors := parseStringOrSlice(m.FilamentColour)
	names := parseStringOrSlice(m.FilamentName)
	weights := parseFloats(m.FilamentWeight)

	if len(types) == 0 && len(names) == 0 {
		return "", "", ""
	}

	bestIdx := -1
	maxWeight := -1.0
	usedTypes := make([]string, 0)
	seenTypes := make(map[string]bool)

	for i, w := range weights {
		if w > 0 {
			if i < len(types) && types[i] != "" {
				t := types[i]
				if !seenTypes[t] {
					seenTypes[t] = true
					usedTypes = append(usedTypes, t)
				}
			}
			if w > maxWeight {
				maxWeight = w
				bestIdx = i
			}
		}
	}

	// If no positive weights found, fallback to index 0
	if bestIdx == -1 {
		bestIdx = 0
		if len(types) > 0 && types[0] != "" {
			usedTypes = append(usedTypes, types[0])
		}
	}

	if len(usedTypes) > 1 {
		filType = strings.Join(usedTypes, " + ")
	} else if len(usedTypes) == 1 {
		filType = usedTypes[0]
	} else if bestIdx < len(types) {
		filType = types[bestIdx]
	}

	if bestIdx < len(colors) {
		filColor = strings.TrimSpace(colors[bestIdx])
	}
	if bestIdx < len(names) {
		filName = strings.TrimSpace(names[bestIdx])
	}

	return filType, filColor, filName
}

// MoonrakerStatus is the unified state aggregated from printer objects
type MoonrakerStatus struct {
	KlippyState    string
	PrintState     string // "standby", "printing", "paused", "complete", "error", "cancelled"
	Filename       string
	Progress       float64 // 0.0 to 1.0
	PrintDuration  float64 // seconds
	TotalDuration  float64 // seconds
	CurrentLayer   int
	TotalLayers    int
	ExtruderTemp   float64
	ExtruderTarget float64
	BedTemp        float64
	BedTarget      float64
	EstimatedTime  float64 // seconds from metadata or calculation
	FilamentType   string  // e.g. "PETG", "PLA + PETG"
	FilamentColor  string  // e.g. "#8E24AA"
	FilamentName   string  // e.g. "Generic PETG"
	Message        string
}

func parseStringOrSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case string:
		// Could be "PLA;PETG" or "Generic PLA\";\"Generic PETG"
		parts := strings.Split(val, ";")
		res := make([]string, 0, len(parts))
		for _, p := range parts {
			cleaned := strings.Trim(p, ` "';`)
			cleaned = strings.TrimSpace(cleaned)
			if cleaned != "" {
				res = append(res, cleaned)
			}
		}
		return res
	case []interface{}:
		res := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				cleaned := strings.Trim(s, ` "';`)
				cleaned = strings.TrimSpace(cleaned)
				if cleaned != "" {
					res = append(res, cleaned)
				}
			}
		}
		return res
	case []string:
		return val
	}
	return nil
}

func parseFloats(v interface{}) []float64 {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case float64:
		return []float64{val}
	case int:
		return []float64{float64(val)}
	case string:
		parts := strings.Split(val, ";")
		res := make([]float64, 0, len(parts))
		for _, p := range parts {
			if num, err := strconv.ParseFloat(strings.TrimSpace(p), 64); err == nil {
				res = append(res, num)
			}
		}
		return res
	case []interface{}:
		res := make([]float64, 0, len(val))
		for _, item := range val {
			switch num := item.(type) {
			case float64:
				res = append(res, num)
			case int:
				res = append(res, float64(num))
			case string:
				if n, err := strconv.ParseFloat(strings.TrimSpace(num), 64); err == nil {
					res = append(res, n)
				}
			}
		}
		return res
	case []float64:
		return val
	}
	return nil
}
