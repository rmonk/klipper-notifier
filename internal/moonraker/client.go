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

	mu            sync.Mutex
	status        MoonrakerStatus
	metadata      map[string]*GCodeMetadata
	metaFetching  map[string]bool
	afcLanes      map[string]*AFCLane
	afcLaneNames  map[string]struct{}
	afcDiscovered bool
	afcRoot       *AFCObject
	listeners     []func(MoonrakerStatus)
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
		metadata:     make(map[string]*GCodeMetadata),
		metaFetching: make(map[string]bool),
		afcLanes:     make(map[string]*AFCLane),
		afcLaneNames: make(map[string]struct{}),
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

// GetMetadata fetches metadata for a sliced gcode file. Results are cached in memory.
// Concurrent callers for the same filename receive (nil, nil) while a fetch is in-flight,
// which does not mean metadata is absent; callers should rely on ensureFileMetadata
// which notifies listeners once metadata resolution completes.
func (c *Client) GetMetadata(ctx context.Context, filename string) (*GCodeMetadata, error) {
	if filename == "" {
		return nil, nil
	}

	c.mu.Lock()
	if meta, ok := c.metadata[filename]; ok {
		c.mu.Unlock()
		return meta, nil
	}
	if c.metaFetching[filename] {
		c.mu.Unlock()
		return nil, nil
	}
	c.metaFetching[filename] = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.metaFetching, filename)
		c.mu.Unlock()
	}()

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

// ensureFileMetadata pulls gcode metadata once per print/file and applies ETA/layers and filament fallback if no AFC.
func (c *Client) ensureFileMetadata(ctx context.Context, filename string) {
	if filename == "" {
		return
	}

	meta, err := c.GetMetadata(ctx, filename)
	if err != nil || meta == nil {
		return
	}

	c.mu.Lock()
	if meta.LayerCount > 0 && c.status.TotalLayers == 0 {
		c.status.TotalLayers = meta.LayerCount
	}
	if meta.EstimatedTime > 0 && c.status.EstimatedTime == 0 {
		c.status.EstimatedTime = meta.EstimatedTime
	}
	// If no AFC was detected or AFC gave no filament info, fallback to gcode metadata
	if c.status.FilamentType == "" {
		fType, fColor, fName := meta.GetFilamentInfo()
		if fType != "" {
			c.status.FilamentType = fType
		}
		if fColor != "" && c.status.FilamentColor == "" {
			c.status.FilamentColor = fColor
		}
		if fName != "" && c.status.FilamentName == "" {
			c.status.FilamentName = fName
		}
	}
	updated := c.status
	c.mu.Unlock()

	c.notifyListeners(updated)
}

// ListObjects queries /printer/objects/list to dynamically discover available printer objects.
func (c *Client) ListObjects(ctx context.Context) ([]string, error) {
	u := c.httpBaseURL + "/printer/objects/list"
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
		return nil, fmt.Errorf("printer/objects/list returned status %d", resp.StatusCode)
	}

	var res struct {
		Result struct {
			Objects []string `json:"objects"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res.Result.Objects, nil
}

// QueryStatus queries current printer status via REST.
func (c *Client) QueryStatus(ctx context.Context) (*MoonrakerStatus, error) {
	queryObjs := []string{"print_stats", "display_status", "virtual_sdcard", "heater_bed", "extruder", "toolhead", "AFC"}

	c.mu.Lock()
	discovered := c.afcDiscovered
	c.mu.Unlock()
	if !discovered {
		if objects, err := c.ListObjects(ctx); err == nil {
			c.mu.Lock()
			for _, object := range objects {
				if strings.HasPrefix(object, "AFC_lane") {
					c.afcLaneNames[object] = struct{}{}
				}
			}
			c.afcDiscovered = true
			c.mu.Unlock()
		}
	}

	// Dynamically include any discovered AFC_lane objects.
	c.mu.Lock()
	for laneName := range c.afcLaneNames {
		queryObjs = append(queryObjs, url.QueryEscape(laneName))
	}
	c.mu.Unlock()

	u := c.httpBaseURL + "/printer/objects/query?" + strings.Join(queryObjs, "&")
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
			Status map[string]json.RawMessage `json:"status"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.updateFromRawStatus(res.Result.Status)
	current := c.status
	c.mu.Unlock()

	if current.Filename != "" {
		c.ensureFileMetadata(ctx, current.Filename)
		c.mu.Lock()
		current = c.status
		c.mu.Unlock()
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

	// Discover and subscribe to printer objects
	subObjects := map[string]interface{}{
		"print_stats":    nil,
		"display_status": nil,
		"virtual_sdcard": nil,
		"heater_bed":     nil,
		"extruder":       nil,
		"toolhead":       nil,
		"AFC":            nil,
	}

	if objs, err := c.ListObjects(ctx); err == nil {
		for _, o := range objs {
			if strings.HasPrefix(o, "AFC_lane") {
				subObjects[o] = nil
			}
		}
	}

	subReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "printer.objects.subscribe",
		"params": map[string]interface{}{
			"objects": subObjects,
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
		var params []map[string]json.RawMessage
		if err := json.Unmarshal(raw.Params, &params); err == nil && len(params) > 0 {
			rawObjects := params[0]
			c.mu.Lock()
			c.updateFromRawStatus(rawObjects)
			st := c.status
			c.mu.Unlock()

			if st.Filename != "" {
				go c.ensureFileMetadata(ctx, st.Filename)
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

func (c *Client) updateFromRawStatus(objects map[string]json.RawMessage) {
	if raw, ok := objects["print_stats"]; ok {
		var ps PrintStats
		if err := json.Unmarshal(raw, &ps); err == nil {
			if ps.Filename != "" && ps.Filename != c.status.Filename {
				c.status.Filename = ps.Filename
				c.status.FilamentType = ""
				c.status.FilamentColor = ""
				c.status.FilamentName = ""
				c.status.TotalLayers = 0
				c.status.EstimatedTime = 0
			}
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
	}

	if raw, ok := objects["display_status"]; ok {
		var ds DisplayStatus
		if err := json.Unmarshal(raw, &ds); err == nil {
			if ds.Progress > 0 {
				c.status.Progress = ds.Progress
			}
			if ds.Message != "" {
				c.status.Message = ds.Message
			}
		}
	}

	if raw, ok := objects["virtual_sdcard"]; ok {
		var vs VirtualSDCard
		if err := json.Unmarshal(raw, &vs); err == nil {
			if vs.Progress > 0 && c.status.Progress == 0 {
				c.status.Progress = vs.Progress
			}
		}
	}

	if raw, ok := objects["heater_bed"]; ok {
		var hb HeaterBed
		if err := json.Unmarshal(raw, &hb); err == nil {
			c.status.BedTemp = hb.Temperature
			c.status.BedTarget = hb.Target
		}
	}

	if raw, ok := objects["extruder"]; ok {
		var ex Extruder
		if err := json.Unmarshal(raw, &ex); err == nil {
			c.status.ExtruderTemp = ex.Temperature
			c.status.ExtruderTarget = ex.Target
		}
	}

	var currentExtruder string
	if raw, ok := objects["toolhead"]; ok {
		var th Toolhead
		if err := json.Unmarshal(raw, &th); err == nil {
			if th.Extruder != "" {
				currentExtruder = th.Extruder
			}
			if len(th.Position) >= 3 {
				z := th.Position[2]
				if c.status.CurrentLayer == 0 && z > 0 {
					c.status.CurrentLayer = int(z / 0.2)
				}
			}
		}
	}

	// Parse AFC root object
	if raw, ok := objects["AFC"]; ok {
		var afc AFCObject
		if err := json.Unmarshal(raw, &afc); err == nil {
			c.afcRoot = &afc
		}
	}

	// Parse all AFC_lane objects
	for k, v := range objects {
		if strings.HasPrefix(k, "AFC_lane") {
			var lane AFCLane
			if err := json.Unmarshal(v, &lane); err == nil {
				c.afcLanes[k] = &lane
				if lane.Name != "" {
					c.afcLanes[lane.Name] = &lane
				}
			}
		}
	}

	// Resolve active filament from AFC
	var activeLane *AFCLane

	// 1. Check current_load or current_lane from AFC state
	if c.afcRoot != nil {
		targetLane := c.afcRoot.CurrentLoad
		if targetLane == "" {
			targetLane = c.afcRoot.CurrentLane
		}
		if targetLane != "" {
			if lane, ok := c.afcLanes[targetLane]; ok {
				activeLane = lane
			} else if lane, ok := c.afcLanes["AFC_lane "+targetLane]; ok {
				activeLane = lane
			}
		}
	}

	// 2. Fallback: check lanes marked as Loaded or Tooled
	if activeLane == nil {
		for _, lane := range c.afcLanes {
			stLower := strings.ToLower(lane.Status)
			if lane.ToolLoaded || stLower == "loaded" || stLower == "tooled" {
				if currentExtruder == "" || lane.Extruder == currentExtruder {
					activeLane = lane
					break
				}
			}
		}
	}

	if activeLane != nil {
		if activeLane.Material != "" {
			c.status.FilamentType = activeLane.Material
		}
		if activeLane.Color != "" {
			c.status.FilamentColor = activeLane.Color
		}
		if activeLane.FilamentName != "" {
			c.status.FilamentName = activeLane.FilamentName
		}
	}
}

func (c *Client) applyAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
}
