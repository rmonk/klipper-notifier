package notify

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var hexColorRegex = regexp.MustCompile(`^#?[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$`)

// MetricChip represents a small metric badge shown on iOS Live Activity widget.
type MetricChip struct {
	Label  string `json:"label,omitempty"`
	Value  string `json:"value"`
	Unit   string `json:"unit,omitempty"`
	Symbol string `json:"symbol,omitempty"`
	Color  string `json:"color,omitempty"`
}

// TileButton represents an interactive button on the Live Activity.
type TileButton struct {
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
	Color string `json:"color,omitempty"`
}

// TileContent represents the visual payload sent to Notify! for Live Activities.
type TileContent struct {
	Title    string       `json:"title,omitempty"`
	Body     string       `json:"body,omitempty"`
	Status   string       `json:"status,omitempty"`
	Symbol   string       `json:"symbol,omitempty"`
	Color    string       `json:"color,omitempty"`
	Tint     string       `json:"tint,omitempty"`
	Progress *int         `json:"progress,omitempty"`
	EndsIn   *int         `json:"endsIn,omitempty"`
	Trailing string       `json:"trailing,omitempty"`
	Metrics  []MetricChip `json:"metrics,omitempty"`
	Buttons  []TileButton `json:"buttons,omitempty"`
}

// adjustContrast computes the perceived luminance of a hex color and remaps
// extreme darks (black) and extreme lights (white) to accessible contrast-friendly shades
// that stay legible across both Dark and Light iOS Live Activity themes.
func adjustContrast(hexStr string) string {
	clean := strings.TrimPrefix(hexStr, "#")
	if len(clean) < 6 {
		return "#" + clean
	}

	r, err1 := strconv.ParseUint(clean[0:2], 16, 8)
	g, err2 := strconv.ParseUint(clean[2:4], 16, 8)
	b, err3 := strconv.ParseUint(clean[4:6], 16, 8)
	if err1 != nil || err2 != nil || err3 != nil {
		return "#" + clean[:6]
	}

	// Standard perceived luminance formula (ITU-R BT.601)
	lum := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)

	// Black and near-black (Y < 50): remap to Apple systemGray (#8E8E93)
	// Prevents invisible 0:1 contrast against black Dynamic Island and dark frosted glass.
	if lum < 50 {
		return "#8E8E93"
	}

	// White and near-white (Y > 230): remap to Apple systemGray4 (#D1D1D6)
	// Prevents washing out on light Lock Screen backgrounds while staying crisp on dark widgets.
	if lum > 230 {
		return "#D1D1D6"
	}

	return "#" + clean[:6]
}

// FormatTint normalizes a color name or hex code into a valid #RRGGBB tint hex string for Notify!
// Returns fallback "#00A76F" (teal) if the color is unparseable.
func FormatTint(c string) string {
	c = strings.TrimSpace(c)
	if c == "" {
		return ""
	}
	switch strings.ToLower(c) {
	case "black":
		return "#8E8E93" // Charcoal / Slate for readability on black Dynamic Island
	case "white":
		return "#D1D1D6" // Silver / Platinum for readability on light backgrounds
	case "teal":
		return "#00A76F"
	case "green":
		return "#34C759"
	case "orange":
		return "#FF9500"
	case "red":
		return "#FF3B30"
	case "blue":
		return "#007AFF"
	case "purple":
		return "#AF52DE"
	case "indigo":
		return "#5856D6"
	case "pink":
		return "#FF2D55"
	case "yellow":
		return "#FFCC00"
	case "gray", "grey":
		return "#8E8E93"
	}
	if hexColorRegex.MatchString(c) {
		return adjustContrast(c)
	}
	return "#00A76F"
}

// PushNotification represents an ordinary push notification.
type PushNotification struct {
	Title         string `json:"title"`
	Text          string `json:"text"`
	GroupType     string `json:"groupType,omitempty"`
	IconURL       string `json:"iconUrl,omitempty"`
	TimeSensitive bool   `json:"timeSensitive,omitempty"`
}

// StartedActivity represents the response from starting a Live Activity.
type StartedActivity struct {
	ActivityID string     `json:"activityId"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

// ActivityStatus represents the status of an existing Live Activity.
type ActivityStatus struct {
	ActivityID string     `json:"activityId"`
	State      string     `json:"state"` // "starting", "active", "dismissed", "ended"
	EndReason  *string    `json:"endReason,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

// LinkResponse represents the response from GET /link.
type LinkResponse struct {
	Success bool   `json:"success"`
	Type    string `json:"type"` // "device" or "group"
	Name    string `json:"name,omitempty"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// NotifyError is the base error type for Notify! API errors.
type NotifyError struct {
	StatusCode int
	Message    string
	Code       string
	Retryable  bool
}

func (e *NotifyError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("notify API error (status %d, code %s): %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("notify API error (status %d): %s", e.StatusCode, e.Message)
}
