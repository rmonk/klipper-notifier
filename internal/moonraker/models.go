package moonraker

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
}

// GCodeMetadata represents metadata returned by /server/files/metadata
type GCodeMetadata struct {
	EstimatedTime    float64 `json:"estimated_time"`
	LayerHeight      float64 `json:"layer_height"`
	FirstLayerHeight float64 `json:"first_layer_height"`
	LayerCount       int     `json:"layer_count"`
	ObjectHeight     float64 `json:"object_height"`
	FilamentTotal    float64 `json:"filament_total"`
}

// MoonrakerStatus is the unified state aggregated from printer objects
type MoonrakerStatus struct {
	KlippyState   string
	PrintState    string // "standby", "printing", "paused", "complete", "error", "cancelled"
	Filename      string
	Progress      float64 // 0.0 to 1.0
	PrintDuration float64 // seconds
	TotalDuration float64 // seconds
	CurrentLayer  int
	TotalLayers   int
	ExtruderTemp  float64
	ExtruderTarget float64
	BedTemp       float64
	BedTarget     float64
	EstimatedTime float64 // seconds from metadata or calculation
	Message       string
}
