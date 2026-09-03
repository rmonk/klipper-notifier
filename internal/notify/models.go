package notify

import (
	"fmt"
	"regexp"
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

// FormatTint normalizes a color name or hex code into a valid #RRGGBB tint hex string for Notify!
// Returns fallback "#00A76F" (teal) if the color is unparseable.
func FormatTint(c string) string {
	c = strings.TrimSpace(c)
	if c == "" {
		return ""
	}
	switch strings.ToLower(c) {
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
		if !strings.HasPrefix(c, "#") {
			return "#" + c
		}
		return c
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
