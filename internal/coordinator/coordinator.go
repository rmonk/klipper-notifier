package coordinator

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/printer-notifier/notify-klipper/internal/config"
	"github.com/printer-notifier/notify-klipper/internal/moonraker"
	"github.com/printer-notifier/notify-klipper/internal/notify"
)

type Coordinator struct {
	cfg        *config.Config
	moonClient *moonraker.Client
	notifCli   *notify.Client

	mu            sync.Mutex
	activityID    string
	lastState     string
	lastJobName   string
	lastProgress  int
	lastUpdate    time.Time
	startTime     time.Time
	isPaused      bool
	lastErrorMsg  string
}

func NewCoordinator(cfg *config.Config, moonClient *moonraker.Client, notifCli *notify.Client) *Coordinator {
	return &Coordinator{
		cfg:        cfg,
		moonClient: moonClient,
		notifCli:   notifCli,
	}
}

// Start begins coordinator polling and event handling.
func (c *Coordinator) Start(ctx context.Context) {
	// Register listener for real-time WebSocket events
	c.moonClient.AddListener(func(st moonraker.MoonrakerStatus) {
		c.HandleStatus(ctx, st)
	})

	// Also launch periodic poll as fallback / keepalive
	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.cleanup(context.Background())
			return
		case <-ticker.C:
			st, err := c.moonClient.QueryStatus(ctx)
			if err != nil {
				if c.cfg.Verbose {
					log.Printf("[Coordinator] Error polling Moonraker: %v", err)
				}
				continue
			}
			c.HandleStatus(ctx, *st)
		}
	}
}

func (c *Coordinator) HandleStatus(ctx context.Context, st moonraker.MoonrakerStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()

	currentState := strings.ToLower(st.PrintState)
	if currentState == "" {
		currentState = "standby"
	}

	jobName := cleanJobName(st.Filename)
	progressPct := int(st.Progress * 100)
	if progressPct < 0 {
		progressPct = 0
	} else if progressPct > 100 {
		progressPct = 100
	}

	// 1. Detect transition to PRINTING
	if currentState == "printing" {
		if c.activityID == "" || c.lastState != "printing" && c.lastState != "paused" {
			c.startPrint(ctx, jobName, progressPct, st)
		} else if c.isPaused {
			// Resumed from paused
			c.isPaused = false
			c.updateTile(ctx, "Printing", "teal", "printer.fill", progressPct, jobName, st)
		} else {
			// Periodic progress update
			if time.Since(c.lastUpdate) >= 5*time.Second || progressPct != c.lastProgress {
				c.updateTile(ctx, "Printing", "teal", "printer.fill", progressPct, jobName, st)
			}
		}
	} else if currentState == "paused" {
		if !c.isPaused {
			c.isPaused = true
			c.pausePrint(ctx, jobName, progressPct, st)
		}
	} else if currentState == "complete" {
		if c.activityID != "" && c.lastState != "complete" {
			c.completePrint(ctx, jobName, st)
		}
	} else if currentState == "cancelled" {
		if c.activityID != "" && c.lastState != "cancelled" {
			c.cancelPrint(ctx, jobName, st)
		}
	} else if currentState == "error" {
		if c.activityID != "" && c.lastState != "error" {
			c.errorPrint(ctx, jobName, st)
		}
	} else if currentState == "standby" {
		if c.activityID != "" && (c.lastState == "complete" || c.lastState == "cancelled" || c.lastState == "error") {
			// Clear finished activity
			c.activityID = ""
			c.isPaused = false
		}
	}

	c.lastState = currentState
	c.lastJobName = jobName
	c.lastProgress = progressPct
}

func (c *Coordinator) startPrint(ctx context.Context, jobName string, progressPct int, st moonraker.MoonrakerStatus) {
	c.startTime = time.Now()
	c.isPaused = false
	log.Printf("[Coordinator] Print started: %s (printer: %s)", jobName, c.cfg.PrinterName)

	endsIn := calculateEndsIn(st)

	tile := notify.TileContent{
		Title:    c.cfg.PrinterName,
		Body:     jobName,
		Status:   "Printing",
		Symbol:   "printer.fill",
		Color:    "teal",
		Progress: &progressPct,
		EndsIn:   endsIn,
	}

	if c.cfg.ShowMetrics {
		tile.Metrics = buildMetrics(st)
	}
	if c.cfg.ShowTrailingLayer && st.TotalLayers > 0 {
		tile.Trailing = fmt.Sprintf("Layer %d/%d", st.CurrentLayer, st.TotalLayers)
	}

	started, err := c.notifCli.Start(ctx, tile)
	if err != nil {
		log.Printf("[Coordinator] Failed to start Live Activity: %v", err)
	} else {
		c.activityID = started.ActivityID
		c.lastUpdate = time.Now()
		log.Printf("[Coordinator] Live Activity started: ID=%s", c.activityID)
	}

	if c.cfg.EnablePushNotifications {
		_ = c.notifCli.Notify(ctx, notify.PushNotification{
			Title:     c.cfg.PrinterName,
			Text:      fmt.Sprintf("Print Started: %s", jobName),
			GroupType: c.cfg.PrinterName,
		})
	}
}

func (c *Coordinator) updateTile(ctx context.Context, status, color, symbol string, progressPct int, jobName string, st moonraker.MoonrakerStatus) {
	if c.activityID == "" {
		return
	}

	endsIn := calculateEndsIn(st)

	tile := notify.TileContent{
		Title:    c.cfg.PrinterName,
		Body:     jobName,
		Status:   status,
		Symbol:   symbol,
		Color:    color,
		Progress: &progressPct,
		EndsIn:   endsIn,
	}

	if c.cfg.ShowMetrics {
		tile.Metrics = buildMetrics(st)
	}
	if c.cfg.ShowTrailingLayer && st.TotalLayers > 0 {
		tile.Trailing = fmt.Sprintf("Layer %d/%d", st.CurrentLayer, st.TotalLayers)
	}

	if err := c.notifCli.Update(ctx, c.activityID, tile); err != nil {
		log.Printf("[Coordinator] Failed to update Live Activity %s: %v", c.activityID, err)
	} else {
		c.lastUpdate = time.Now()
	}
}

func (c *Coordinator) pausePrint(ctx context.Context, jobName string, progressPct int, st moonraker.MoonrakerStatus) {
	log.Printf("[Coordinator] Print paused: %s", jobName)
	c.updateTile(ctx, "Paused", "orange", "pause.circle.fill", progressPct, jobName, st)

	if c.cfg.EnablePushNotifications {
		_ = c.notifCli.Notify(ctx, notify.PushNotification{
			Title:         c.cfg.PrinterName,
			Text:          fmt.Sprintf("Print Paused: %s at %d%%", jobName, progressPct),
			GroupType:     c.cfg.PrinterName,
			TimeSensitive: true,
		})
	}
}

func (c *Coordinator) completePrint(ctx context.Context, jobName string, st moonraker.MoonrakerStatus) {
	log.Printf("[Coordinator] Print complete: %s", jobName)
	prog := 100
	durationStr := formatDuration(st.PrintDuration)

	tile := notify.TileContent{
		Title:    c.cfg.PrinterName,
		Body:     jobName,
		Status:   "Done",
		Symbol:   "checkmark.circle.fill",
		Color:    "green",
		Progress: &prog,
	}

	if c.cfg.ShowMetrics {
		tile.Metrics = buildMetrics(st)
	}

	if err := c.notifCli.End(ctx, c.activityID, &tile, c.cfg.KeepForSeconds); err != nil {
		log.Printf("[Coordinator] Failed to end Live Activity %s: %v", c.activityID, err)
	}

	if c.cfg.EnablePushNotifications {
		_ = c.notifCli.Notify(ctx, notify.PushNotification{
			Title:     c.cfg.PrinterName,
			Text:      fmt.Sprintf("Print Finished: %s in %s", jobName, durationStr),
			GroupType: c.cfg.PrinterName,
		})
	}

	c.activityID = ""
}

func (c *Coordinator) cancelPrint(ctx context.Context, jobName string, st moonraker.MoonrakerStatus) {
	log.Printf("[Coordinator] Print stopped/cancelled: %s", jobName)
	prog := int(st.Progress * 100)

	tile := notify.TileContent{
		Title:    c.cfg.PrinterName,
		Body:     jobName,
		Status:   "Stopped",
		Symbol:   "xmark.circle.fill",
		Color:    "gray",
		Progress: &prog,
	}

	_ = c.notifCli.End(ctx, c.activityID, &tile, 60)
	c.activityID = ""
}

func (c *Coordinator) errorPrint(ctx context.Context, jobName string, st moonraker.MoonrakerStatus) {
	log.Printf("[Coordinator] Print failed: %s (message: %s)", jobName, st.Message)
	prog := int(st.Progress * 100)

	tile := notify.TileContent{
		Title:    c.cfg.PrinterName,
		Body:     jobName,
		Status:   "Failed",
		Symbol:   "exclamationmark.triangle.fill",
		Color:    "red",
		Progress: &prog,
	}

	_ = c.notifCli.End(ctx, c.activityID, &tile, c.cfg.KeepForSeconds)

	if c.cfg.EnablePushNotifications {
		errMsg := st.Message
		if errMsg == "" {
			errMsg = "An error occurred on the printer"
		}
		_ = c.notifCli.Notify(ctx, notify.PushNotification{
			Title:         c.cfg.PrinterName,
			Text:          fmt.Sprintf("Print Failed: %s (%s)", jobName, errMsg),
			GroupType:     c.cfg.PrinterName,
			TimeSensitive: true,
		})
	}

	c.activityID = ""
}

func (c *Coordinator) cleanup(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.activityID != "" {
		_ = c.notifCli.End(ctx, c.activityID, nil, 0)
		c.activityID = ""
	}
}

func cleanJobName(raw string) string {
	if raw == "" {
		return "Untitled Print"
	}
	name := filepath.Base(raw)
	name = strings.TrimSuffix(name, ".gcode.3mf")
	name = strings.TrimSuffix(name, ".gcode")
	name = strings.TrimSuffix(name, ".3mf")
	name = strings.TrimSuffix(name, ".stl")
	return strings.TrimSpace(name)
}

func calculateEndsIn(st moonraker.MoonrakerStatus) *int {
	var remainingSec float64
	if st.EstimatedTime > 0 && st.PrintDuration > 0 {
		remainingSec = st.EstimatedTime - st.PrintDuration
	} else if st.Progress > 0.05 && st.PrintDuration > 10 {
		totalEst := st.PrintDuration / st.Progress
		remainingSec = totalEst - st.PrintDuration
	}

	if remainingSec >= 1 && remainingSec <= 86400 {
		sec := int(remainingSec)
		return &sec
	}
	return nil
}

func buildMetrics(st moonraker.MoonrakerStatus) []notify.MetricChip {
	var metrics []notify.MetricChip
	if st.ExtruderTemp > 0 {
		val := fmt.Sprintf("%.0f°", st.ExtruderTemp)
		if st.ExtruderTarget > 0 {
			val = fmt.Sprintf("%.0f/%.0f°", st.ExtruderTemp, st.ExtruderTarget)
		}
		metrics = append(metrics, notify.MetricChip{
			Label: "Nozzle",
			Value: val,
		})
	}
	if st.BedTemp > 0 {
		val := fmt.Sprintf("%.0f°", st.BedTemp)
		if st.BedTarget > 0 {
			val = fmt.Sprintf("%.0f/%.0f°", st.BedTemp, st.BedTarget)
		}
		metrics = append(metrics, notify.MetricChip{
			Label: "Bed",
			Value: val,
		})
	}
	if st.TotalLayers > 0 {
		metrics = append(metrics, notify.MetricChip{
			Label: "Layer",
			Value: fmt.Sprintf("%d/%d", st.CurrentLayer, st.TotalLayers),
		})
	}
	return metrics
}

func formatDuration(sec float64) string {
	d := time.Duration(sec) * time.Second
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	if mins > 0 {
		return fmt.Sprintf("%dm %ds", mins, secs)
	}
	return fmt.Sprintf("%ds", secs)
}
