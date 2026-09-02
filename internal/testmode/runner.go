package testmode

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/printer-notifier/notify-klipper/internal/config"
	"github.com/printer-notifier/notify-klipper/internal/moonraker"
	"github.com/printer-notifier/notify-klipper/internal/notify"
)

type Runner struct {
	cfg        *config.Config
	moonClient *moonraker.Client
	notifCli   *notify.Client
	interval   time.Duration
}

func NewRunner(cfg *config.Config, moonClient *moonraker.Client, notifCli *notify.Client) *Runner {
	return &Runner{
		cfg:        cfg,
		moonClient: moonClient,
		notifCli:   notifCli,
		interval:   15 * time.Second,
	}
}

// SetInterval allows customizing interval for unit testing.
func (r *Runner) SetInterval(d time.Duration) {
	r.interval = d
}

// Run executes the test validation and fake print job.
func (r *Runner) Run(ctx context.Context) error {
	fmt.Println("==================================================")
	fmt.Println("       notify-klipper TEST MODE EXECUTION         ")
	fmt.Println("==================================================")

	// Step 1: Validate Moonraker Connection & API Key
	fmt.Println("\n[1/3] Validating Moonraker connection...")
	if r.moonClient != nil {
		serverInfo, err := r.moonClient.CheckConnection(ctx)
		if err != nil {
			if r.cfg.DryRun {
				fmt.Printf("  [!] Moonraker check warning (dry-run mode): %v\n", err)
			} else {
				return fmt.Errorf("moonraker validation failed: %w", err)
			}
		} else {
			if r.cfg.MoonrakerAPIKey == "" {
				fmt.Printf("  [✓] Successfully connected to Moonraker at %s (Moonraker %s, Klippy: %s, No API key required)\n",
					r.cfg.MoonrakerURL, serverInfo.Version, serverInfo.KlippyState)
			} else {
				fmt.Printf("  [✓] Successfully authenticated with Moonraker at %s (Moonraker %s, Klippy: %s)\n",
					r.cfg.MoonrakerURL, serverInfo.Version, serverInfo.KlippyState)
			}
		}
	} else {
		fmt.Println("  [~] Moonraker check skipped (no client provided)")
	}

	// Step 2: Validate Notify! Credentials
	fmt.Println("\n[2/3] Validating Notify! credentials with https://push.getnotifyapp.com/link...")
	linkResp, err := r.notifCli.Link(ctx)
	if err != nil {
		return fmt.Errorf("notify! credential validation failed: %w", err)
	}
	deviceName := linkResp.Name
	if deviceName == "" {
		deviceName = r.cfg.NotifyDeviceID
	}
	fmt.Printf("  [✓] Notify! credentials verified for iOS Device: %q (ID: %s)\n", deviceName, r.cfg.NotifyDeviceID)

	// Step 3: Send Fake 1-minute print job (1 update every 15 seconds)
	fmt.Printf("\n[3/3] Running fake print job test (Total: %v, updates every %v)...\n", 4*r.interval, r.interval)

	printerName := r.cfg.PrinterName
	if printerName == "" {
		printerName = "Fake Klipper Printer"
	}
	jobName := "3DBenchy_SpeedTest"

	// T = 0s: Start print
	p0 := 0
	e0 := int((4 * r.interval).Seconds())
	fmt.Printf("  -> [T+00s] [0%%] Starting Live Activity tile (ETA: %ds)...\n", e0)

	startTile := notify.TileContent{
		Title:    printerName,
		Body:     jobName,
		Status:   "Printing",
		Symbol:   "printer.fill",
		Color:    "teal",
		Progress: &p0,
		EndsIn:   &e0,
	}

	if r.cfg.ShowMetrics {
		startTile.Metrics = []notify.MetricChip{
			{Label: "Nozzle", Value: "215/215°"},
			{Label: "Bed", Value: "60/60°"},
			{Label: "Layer", Value: "1/100"},
		}
	}
	if r.cfg.ShowTrailingLayer {
		startTile.Trailing = "Layer 1/100"
	}

	started, err := r.notifCli.Start(ctx, startTile)
	if err != nil {
		return fmt.Errorf("failed to start fake live activity: %w", err)
	}
	activityID := started.ActivityID
	fmt.Printf("     Live Activity active: ID=%s\n", activityID)

	if r.cfg.EnablePushNotifications {
		_ = r.notifCli.Notify(ctx, notify.PushNotification{
			Title:     printerName,
			Text:      fmt.Sprintf("Print Started: %s", jobName),
			GroupType: printerName,
		})
	}

	// Wait 15s
	if err := sleepContext(ctx, r.interval); err != nil {
		return err
	}

	// T = 15s: 25%
	p25 := 25
	e25 := int((3 * r.interval).Seconds())
	fmt.Printf("  -> [T+15s] [25%%] Updating Live Activity tile (ETA: %ds)...\n", e25)
	tile25 := notify.TileContent{
		Title:    printerName,
		Body:     jobName,
		Status:   "Printing",
		Symbol:   "printer.fill",
		Color:    "teal",
		Progress: &p25,
		EndsIn:   &e25,
	}
	if r.cfg.ShowMetrics {
		tile25.Metrics = []notify.MetricChip{
			{Label: "Nozzle", Value: "215°"},
			{Label: "Bed", Value: "60°"},
			{Label: "Layer", Value: "25/100"},
		}
	}
	if r.cfg.ShowTrailingLayer {
		tile25.Trailing = "Layer 25/100"
	}
	if err := r.notifCli.Update(ctx, activityID, tile25); err != nil {
		log.Printf("     Warning: tile update failed: %v", err)
	}

	// Wait 15s
	if err := sleepContext(ctx, r.interval); err != nil {
		return err
	}

	// T = 30s: 50%
	p50 := 50
	e50 := int((2 * r.interval).Seconds())
	fmt.Printf("  -> [T+30s] [50%%] Updating Live Activity tile (ETA: %ds)...\n", e50)
	tile50 := notify.TileContent{
		Title:    printerName,
		Body:     jobName,
		Status:   "Printing",
		Symbol:   "printer.fill",
		Color:    "teal",
		Progress: &p50,
		EndsIn:   &e50,
	}
	if r.cfg.ShowMetrics {
		tile50.Metrics = []notify.MetricChip{
			{Label: "Nozzle", Value: "215°"},
			{Label: "Bed", Value: "60°"},
			{Label: "Layer", Value: "50/100"},
		}
	}
	if r.cfg.ShowTrailingLayer {
		tile50.Trailing = "Layer 50/100"
	}
	if err := r.notifCli.Update(ctx, activityID, tile50); err != nil {
		log.Printf("     Warning: tile update failed: %v", err)
	}

	// Wait 15s
	if err := sleepContext(ctx, r.interval); err != nil {
		return err
	}

	// T = 45s: 75%
	p75 := 75
	e75 := int(r.interval.Seconds())
	fmt.Printf("  -> [T+45s] [75%%] Updating Live Activity tile (ETA: %ds)...\n", e75)
	tile75 := notify.TileContent{
		Title:    printerName,
		Body:     jobName,
		Status:   "Printing",
		Symbol:   "printer.fill",
		Color:    "teal",
		Progress: &p75,
		EndsIn:   &e75,
	}
	if r.cfg.ShowMetrics {
		tile75.Metrics = []notify.MetricChip{
			{Label: "Nozzle", Value: "215°"},
			{Label: "Bed", Value: "60°"},
			{Label: "Layer", Value: "75/100"},
		}
	}
	if r.cfg.ShowTrailingLayer {
		tile75.Trailing = "Layer 75/100"
	}
	if err := r.notifCli.Update(ctx, activityID, tile75); err != nil {
		log.Printf("     Warning: tile update failed: %v", err)
	}

	// Wait 15s
	if err := sleepContext(ctx, r.interval); err != nil {
		return err
	}

	// T = 60s: 100% / Complete
	p100 := 100
	fmt.Printf("  -> [T+60s] [100%%] Print Complete! Ending Live Activity (keepFor: 60s)...\n")
	endTile := notify.TileContent{
		Title:    printerName,
		Body:     jobName,
		Status:   "Done",
		Symbol:   "checkmark.circle.fill",
		Color:    "green",
		Progress: &p100,
	}
	if r.cfg.ShowMetrics {
		endTile.Metrics = []notify.MetricChip{
			{Label: "Nozzle", Value: "45°"},
			{Label: "Bed", Value: "30°"},
			{Label: "Layer", Value: "100/100"},
		}
	}
	if r.cfg.ShowTrailingLayer {
		endTile.Trailing = "Complete"
	}

	err = r.notifCli.End(ctx, activityID, &endTile, 60)
	if err != nil {
		log.Printf("     Warning: ending tile failed: %v", err)
	}

	if r.cfg.EnablePushNotifications {
		_ = r.notifCli.Notify(ctx, notify.PushNotification{
			Title:     printerName,
			Text:      fmt.Sprintf("Print Complete: %s finished in 1m 0s", jobName),
			GroupType: printerName,
		})
	}

	fmt.Println("\n==================================================")
	fmt.Println(" [✓] Test Mode Completed Successfully!")
	fmt.Println("==================================================")
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
