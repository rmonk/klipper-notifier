package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Client struct {
	baseURL     string
	deviceID    string
	deviceToken string
	iconURL     string
	httpClient  *http.Client
	dryRun      bool

	mu         sync.Mutex
	dryCounter int
}

func NewClient(baseURL, deviceID, deviceToken, iconURL string, dryRun bool) *Client {
	return &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		deviceID:    deviceID,
		deviceToken: deviceToken,
		iconURL:     iconURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		dryRun: dryRun,
	}
}

// Link validates credentials and checks if the target is a valid device (Live Activities require a device, not a group).
func (c *Client) Link(ctx context.Context) (*LinkResponse, error) {
	u, err := url.Parse(c.baseURL + "/link")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("id", c.deviceID)
	q.Set("token", c.deviceToken)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if c.dryRun {
			log.Printf("[DRY-RUN] Network unreachable for /link check (%v), returning synthetic device", err)
			return &LinkResponse{
				Success: true,
				Type:    "device",
				Name:    "Dry-Run Virtual iPhone",
			}, nil
		}
		return nil, fmt.Errorf("failed to reach Notify gateway: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 404 || resp.StatusCode == 403 {
		return nil, &NotifyError{
			StatusCode: resp.StatusCode,
			Message:    "invalid Notify device ID or token",
		}
	}

	var linkResp LinkResponse
	if err := json.Unmarshal(bodyBytes, &linkResp); err != nil {
		return nil, fmt.Errorf("invalid response from /link: %w", err)
	}

	if !linkResp.Success && linkResp.Error != "" {
		return nil, &NotifyError{
			StatusCode: resp.StatusCode,
			Message:    linkResp.Error,
		}
	}

	if linkResp.Type == "group" {
		return nil, &NotifyError{
			StatusCode: 400,
			Message:    "credentials point to a Notify Group; Live Activities require an individual Device ID",
		}
	}

	return &linkResp, nil
}

// Start initiates a new Live Activity on iOS device.
func (c *Client) Start(ctx context.Context, content TileContent) (*StartedActivity, error) {
	if c.dryRun {
		c.mu.Lock()
		c.dryCounter++
		aid := fmt.Sprintf("LADRY%03d", c.dryCounter)
		c.mu.Unlock()
		log.Printf("[DRY-RUN] Starting Live Activity: title=%q progress=%v status=%q", content.Title, content.Progress, content.Status)
		return &StartedActivity{ActivityID: aid}, nil
	}

	u, err := url.Parse(fmt.Sprintf("%s/live-activity/%s", c.baseURL, url.PathEscape(c.deviceID)))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("token", c.deviceToken)
	q.Set("new", "1")
	u.RawQuery = q.Encode()

	if content.Tint == "" && content.Color != "" {
		content.Tint = FormatTint(content.Color)
	} else if content.Tint != "" {
		content.Tint = FormatTint(content.Tint)
	}

	data, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("transport error starting live activity: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, parseNotifyError(resp.StatusCode, bodyBytes)
	}

	var res struct {
		Success    bool       `json:"success"`
		ActivityID string     `json:"activityId"`
		ExpiresAt  *time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("failed to parse start response: %w", err)
	}
	if res.ActivityID == "" {
		return nil, fmt.Errorf("live activity start returned no activity ID: %s", string(bodyBytes))
	}

	return &StartedActivity{
		ActivityID: res.ActivityID,
		ExpiresAt:  res.ExpiresAt,
	}, nil
}

// Update updates an ongoing Live Activity tile.
func (c *Client) Update(ctx context.Context, activityID string, content TileContent) error {
	if content.Tint == "" && content.Color != "" {
		content.Tint = FormatTint(content.Color)
	} else if content.Tint != "" {
		content.Tint = FormatTint(content.Tint)
	}

	if c.dryRun {
		log.Printf("[DRY-RUN] Updating Live Activity %s: title=%q progress=%v status=%q tint=%q endsIn=%v",
			activityID, content.Title, content.Progress, content.Status, content.Tint, content.EndsIn)
		return nil
	}

	u, err := url.Parse(fmt.Sprintf("%s/live-activity/%s", c.baseURL, url.PathEscape(activityID)))
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("token", c.deviceToken)
	u.RawQuery = q.Encode()

	data, err := json.Marshal(content)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("transport error updating live activity: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return parseNotifyError(resp.StatusCode, bodyBytes)
	}
	return nil
}

// End finishes an existing Live Activity and optionally leaves it on screen for keepFor seconds.
func (c *Client) End(ctx context.Context, activityID string, content *TileContent, keepFor int) error {
	if c.dryRun {
		log.Printf("[DRY-RUN] Ending Live Activity %s (keepFor: %ds)", activityID, keepFor)
		return nil
	}

	u, err := url.Parse(fmt.Sprintf("%s/live-activity/%s", c.baseURL, url.PathEscape(activityID)))
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("token", c.deviceToken)
	u.RawQuery = q.Encode()

	payload := map[string]interface{}{
		"keepFor": keepFor,
	}
	if content != nil {
		if content.Tint == "" && content.Color != "" {
			content.Tint = FormatTint(content.Color)
		} else if content.Tint != "" {
			content.Tint = FormatTint(content.Tint)
		}

		if content.Title != "" {
			payload["title"] = content.Title
		}
		if content.Body != "" {
			payload["body"] = content.Body
		}
		if content.Status != "" {
			payload["status"] = content.Status
		}
		if content.Symbol != "" {
			payload["symbol"] = content.Symbol
		}
		if content.Color != "" {
			payload["color"] = content.Color
		}
		if content.Tint != "" {
			payload["tint"] = content.Tint
		}
		if content.Progress != nil {
			payload["progress"] = *content.Progress
		}
		if len(content.Metrics) > 0 {
			payload["metrics"] = content.Metrics
		}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("transport error ending live activity: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 && resp.StatusCode != 410 { // 410 is already gone/idempotent
		return parseNotifyError(resp.StatusCode, bodyBytes)
	}
	return nil
}

// Status queries the current status of an activity.
func (c *Client) Status(ctx context.Context, activityID string) (*ActivityStatus, error) {
	if c.dryRun {
		return &ActivityStatus{
			ActivityID: activityID,
			State:      "active",
		}, nil
	}

	u, err := url.Parse(fmt.Sprintf("%s/live-activity/%s", c.baseURL, url.PathEscape(activityID)))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("token", c.deviceToken)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("transport error getting live activity status: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, parseNotifyError(resp.StatusCode, bodyBytes)
	}

	var status ActivityStatus
	if err := json.Unmarshal(bodyBytes, &status); err != nil {
		return nil, fmt.Errorf("failed to parse activity status: %w", err)
	}
	return &status, nil
}

// Notify sends a push notification (separate from Live Activity) to the device.
func (c *Client) Notify(ctx context.Context, p PushNotification) error {
	if p.IconURL == "" {
		p.IconURL = c.iconURL
	}

	if c.dryRun {
		log.Printf("[DRY-RUN] Push Notification: Title=%q Text=%q Group=%q TimeSensitive=%v",
			p.Title, p.Text, p.GroupType, p.TimeSensitive)
		return nil
	}

	u, err := url.Parse(fmt.Sprintf("%s/notify-json/%s", c.baseURL, url.PathEscape(c.deviceID)))
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("token", c.deviceToken)
	u.RawQuery = q.Encode()

	data, err := json.Marshal(p)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("transport error sending push notification: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return parseNotifyError(resp.StatusCode, bodyBytes)
	}
	return nil
}

func parseNotifyError(status int, body []byte) *NotifyError {
	var errMap map[string]interface{}
	_ = json.Unmarshal(body, &errMap)

	msg := string(body)
	var code string
	if errMap != nil {
		if m, ok := errMap["message"].(string); ok && m != "" {
			msg = m
		} else if e, ok := errMap["error"].(string); ok && e != "" {
			msg = e
		}
		if c, ok := errMap["code"].(string); ok {
			code = c
		}
	}

	return &NotifyError{
		StatusCode: status,
		Message:    msg,
		Code:       code,
		Retryable:  status >= 500 || status == 429,
	}
}
