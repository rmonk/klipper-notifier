package moonraker

import (
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

	"github.com/gorilla/websocket"
)

type Client struct {
	httpBaseURL string
	wsBaseURL   string
	apiKey      string
	httpClient  *http.Client

	mu        sync.Mutex
	status    MoonrakerStatus
	metadata  map[string]*GCodeMetadata
	listeners []func(MoonrakerStatus)
}

func NewClient(rawURL, apiKey string) (*Client, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid Moonraker URL: %w", err)
	}

	httpScheme := "http"
	wsScheme := "ws"
	if parsed.Scheme == "https" || parsed.Scheme == "wss" {
		httpScheme = "https"
		wsScheme = "wss"
	}

	host := parsed.Host
	if host == "" {
		host = parsed.Path
	}

	httpBase := fmt.Sprintf("%s://%s", httpScheme, host)
	wsBase := fmt.Sprintf("%s://%s/websocket", wsScheme, host)

	return &Client{
		httpBaseURL: httpBase,
		wsBaseURL:   wsBase,
		apiKey:      apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		metadata: make(map[string]*GCodeMetadata),
	}, nil
}

// AddListener adds a callback for status updates.
func (c *Client) AddListener(fn func(MoonrakerStatus)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listeners = append(c.listeners, fn)
}

func (c *Client) notifyListeners(st MoonrakerStatus) {
	c.mu.Lock()
	callbacks := make([]func(MoonrakerStatus), len(c.listeners))
	copy(callbacks, c.listeners)
	c.mu.Unlock()

	for _, cb := range callbacks {
		cb(st)
	}
}

// CheckConnection verifies that Moonraker server is reachable and API key (if provided) is accepted.
func (c *Client) CheckConnection(ctx context.Context) (*ServerInfo, error) {
	u := c.httpBaseURL + "/server/info"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	c.applyAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach Moonraker at %s: %w", c.httpBaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, fmt.Errorf("moonraker authentication failed (status %d): check MOONRAKER_API_KEY", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("moonraker returned unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var res struct {
		Result ServerInfo `json:"result"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("failed to decode /server/info response: %w", err)
	}

	return &res.Result, nil
}

// GetPrinterInfo queries /printer/info.
func (c *Client) GetPrinterInfo(ctx context.Context) (*PrinterInfo, error) {
	u := c.httpBaseURL + "/printer/info"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	c.applyAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("printer/info returned status %d", resp.StatusCode)
	}

	var res struct {
		Result PrinterInfo `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return &res.Result, nil
}

// GetMetadata fetches metadata for a sliced gcode file.
func (c *Client) GetMetadata(ctx context.Context, filename string) (*GCodeMetadata, error) {
	if filename == "" {
		return nil, nil
	}

	c.mu.Lock()
	if meta, ok := c.metadata[filename]; ok {
		c.mu.Unlock()
		return meta, nil
	}
	c.mu.Unlock()

	u := fmt.Sprintf("%s/server/files/metadata?filename=%s", c.httpBaseURL, url.QueryEscape(filename))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	c.applyAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("server/files/metadata returned status %d", resp.StatusCode)
	}

	var res struct {
		Result GCodeMetadata `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.metadata[filename] = &res.Result
	c.mu.Unlock()

	return &res.Result, nil
}

// QueryStatus queries current printer status via REST.
func (c *Client) QueryStatus(ctx context.Context) (*MoonrakerStatus, error) {
	u := c.httpBaseURL + "/printer/objects/query?print_stats&display_status&virtual_sdcard&heater_bed&extruder&toolhead"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	c.applyAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("printer/objects/query returned status %d", resp.StatusCode)
	}

	var res struct {
		Result struct {
			Status struct {
				PrintStats    *PrintStats    `json:"print_stats"`
				DisplayStatus *DisplayStatus `json:"display_status"`
				VirtualSDCard *VirtualSDCard `json:"virtual_sdcard"`
				HeaterBed     *HeaterBed     `json:"heater_bed"`
				Extruder      *Extruder      `json:"extruder"`
				Toolhead      *Toolhead      `json:"toolhead"`
			} `json:"status"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.updateFromObjects(res.Result.Status.PrintStats, res.Result.Status.DisplayStatus,
		res.Result.Status.VirtualSDCard, res.Result.Status.HeaterBed, res.Result.Status.Extruder, res.Result.Status.Toolhead)
	current := c.status
	c.mu.Unlock()

	if current.Filename != "" {
		if meta, _ := c.GetMetadata(ctx, current.Filename); meta != nil {
			c.mu.Lock()
			if meta.LayerCount > 0 && c.status.TotalLayers == 0 {
				c.status.TotalLayers = meta.LayerCount
			}
			if meta.EstimatedTime > 0 && c.status.EstimatedTime == 0 {
				c.status.EstimatedTime = meta.EstimatedTime
			}
			current = c.status
			c.mu.Unlock()
		}
	}

	return &current, nil
}

// StartWebSocket connects to Moonraker WebSocket and subscribes to printer object events.
func (c *Client) StartWebSocket(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := c.runWebSocket(ctx)
		if err != nil && ctx.Err() == nil {
			log.Printf("[Moonraker WS] Disconnected (%v), reconnecting in 3s...", err)
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return
			}
		}
	}
}

func (c *Client) runWebSocket(ctx context.Context) error {
	wsURL := c.wsBaseURL
	header := make(http.Header)
	if c.apiKey != "" {
		header.Set("X-Api-Key", c.apiKey)
		if strings.Contains(wsURL, "?") {
			wsURL += "&token=" + url.QueryEscape(c.apiKey)
		} else {
			wsURL += "?token=" + url.QueryEscape(c.apiKey)
		}
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return fmt.Errorf("websocket dial error: %w", err)
	}
	defer conn.Close()

	log.Printf("[Moonraker WS] Connected to %s", c.wsBaseURL)

	// Subscribe to printer objects
	subReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "printer.objects.subscribe",
		"params": map[string]interface{}{
			"objects": map[string]interface{}{
				"print_stats":    nil,
				"display_status": nil,
				"virtual_sdcard": nil,
				"heater_bed":     nil,
				"extruder":       nil,
				"toolhead":       nil,
			},
		},
		"id": 1,
	}
	if err := conn.WriteJSON(subReq); err != nil {
		return fmt.Errorf("failed to subscribe to printer objects: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		c.handleWSMessage(ctx, message)
	}
}

func (c *Client) handleWSMessage(ctx context.Context, msg []byte) {
	var raw struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(msg, &raw); err != nil {
		return
	}

	if raw.Method == "notify_status_update" {
		var params []struct {
			PrintStats    *PrintStats    `json:"print_stats"`
			DisplayStatus *DisplayStatus `json:"display_status"`
			VirtualSDCard *VirtualSDCard `json:"virtual_sdcard"`
			HeaterBed     *HeaterBed     `json:"heater_bed"`
			Extruder      *Extruder      `json:"extruder"`
			Toolhead      *Toolhead      `json:"toolhead"`
		}
		if err := json.Unmarshal(raw.Params, &params); err == nil && len(params) > 0 {
			p := params[0]
			c.mu.Lock()
			c.updateFromObjects(p.PrintStats, p.DisplayStatus, p.VirtualSDCard, p.HeaterBed, p.Extruder, p.Toolhead)
			st := c.status
			c.mu.Unlock()

			if st.Filename != "" && (st.TotalLayers == 0 || st.EstimatedTime == 0) {
				go func(fn string) {
					if meta, _ := c.GetMetadata(ctx, fn); meta != nil {
						c.mu.Lock()
						if meta.LayerCount > 0 && c.status.TotalLayers == 0 {
							c.status.TotalLayers = meta.LayerCount
						}
						if meta.EstimatedTime > 0 && c.status.EstimatedTime == 0 {
							c.status.EstimatedTime = meta.EstimatedTime
						}
						updated := c.status
						c.mu.Unlock()
						c.notifyListeners(updated)
					}
				}(st.Filename)
			}

			c.notifyListeners(st)
		}
	} else if raw.Method == "notify_klippy_state_changed" {
		var params []struct {
			State string `json:"state_message"`
		}
		if err := json.Unmarshal(raw.Params, &params); err == nil && len(params) > 0 {
			c.mu.Lock()
			c.status.KlippyState = params[0].State
			st := c.status
			c.mu.Unlock()
			c.notifyListeners(st)
		}
	}
}

func (c *Client) updateFromObjects(ps *PrintStats, ds *DisplayStatus, vs *VirtualSDCard, hb *HeaterBed, ex *Extruder, th *Toolhead) {
	if ps != nil {
		if ps.State != "" {
			c.status.PrintState = ps.State
		}
		if ps.Filename != "" {
			c.status.Filename = ps.Filename
		}
		if ps.PrintDuration > 0 {
			c.status.PrintDuration = ps.PrintDuration
		}
		if ps.TotalDuration > 0 {
			c.status.TotalDuration = ps.TotalDuration
		}
		if ps.Message != "" {
			c.status.Message = ps.Message
		}
		if ps.Info.CurrentLayer != nil {
			c.status.CurrentLayer = *ps.Info.CurrentLayer
		}
		if ps.Info.TotalLayer != nil {
			c.status.TotalLayers = *ps.Info.TotalLayer
		}
	}

	if ds != nil {
		if ds.Progress > 0 {
			c.status.Progress = ds.Progress
		}
		if ds.Message != "" {
			c.status.Message = ds.Message
		}
	}

	if vs != nil && vs.Progress > 0 && c.status.Progress == 0 {
		c.status.Progress = vs.Progress
	}

	if hb != nil {
		c.status.BedTemp = hb.Temperature
		c.status.BedTarget = hb.Target
	}

	if ex != nil {
		c.status.ExtruderTemp = ex.Temperature
		c.status.ExtruderTarget = ex.Target
	}

	if th != nil && len(th.Position) >= 3 {
		// Toolhead Z position can help estimate layer if not reported in print_stats
		z := th.Position[2]
		if c.status.CurrentLayer == 0 && z > 0 {
			// rough approximation: if layer height 0.2, layer = z / 0.2
			c.status.CurrentLayer = int(z / 0.2)
		}
	}
}

func (c *Client) applyAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
}
