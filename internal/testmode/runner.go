package testmode

import (
	"context"
	"fmt"
	"log"
	"math/rand"
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

type testFilament struct {
	material string
	color    string
	name     string
}

var sampleFilaments = []testFilament{
	{material: "PLA", color: "#1E88E5", name: "Generic Blue PLA"},
	{material: "PETG", color: "#8E24AA", name: "Generic Purple PETG"},
	{material: "TPU", color: "#E53935", name: "Generic Red TPU"},
	{material: "ABS", color: "#43A047", name: "Generic Green ABS"},
	{material: "ASA", color: "#FB8C00", name: "Generic Orange ASA"},
	{material: "PLA-CF", color: "#D81B60", name: "Generic Pink PLA-CF"},
	{material: "PETG", color: "#00ACC1", name: "Generic Teal PETG"},
	{material: "PLA", color: "#FDD835", name: "Generic Yellow PLA"},
	{material: "PLA", color: "#000000", name: "Generic Black PLA"},
	{material: "PETG", color: "#FFFFFF", name: "Generic White PETG"},
}

func pickNextFilament(r *rand.Rand, prevIdx int) (testFilament, int) {
	candidates := make([]int, 0, len(sampleFilaments)-1)
	for i := range sampleFilaments {
		if i != prevIdx {
			if prevIdx < 0 || (sampleFilaments[i].color != sampleFilaments[prevIdx].color && sampleFilaments[i].material != sampleFilaments[prevIdx].material) {
				candidates = append(candidates, i)
			}
		}
	}
	if len(candidates) == 0 {
		for i := range sampleFilaments {
			if i != prevIdx {
				candidates = append(candidates, i)
			}
		}
	}
	nextIdx := candidates[r.Intn(len(candidates))]
	return sampleFilaments[nextIdx], nextIdx
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
	jobName := "3DBenchy_MultiColor"
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// T = 0s: Start print
	p0 := 0
	e0 := int((4 * r.interval).Seconds())
	fil0, filIdx := pickNextFilament(rng, -1)
	body0 := fmt.Sprintf("%s • %s", jobName, fil0.material)
	fmt.Printf("  -> [T+00s] [0%%] Starting Live Activity tile (ETA: %ds, Filament: %s, Color: %s)...\n", e0, fil0.material, fil0.color)

	startTile := notify.TileContent{
		Title:    printerName,
		Body:     body0,
		Status:   "Printing",
		Symbol:   "printer.fill",
		Color:    fil0.color,
		Tint:     notify.FormatTint(fil0.color),
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
			Text:      fmt.Sprintf("Print Started: %s", body0),
			GroupType: printerName,
		})
	}

	// Wait 15s
	if err := sleepContext(ctx, r.interval); err != nil {
		return err
	}

	// T = 15s: 25% (Filament Change #1)
	p25 := 25
	e25 := int((3 * r.interval).Seconds())
	fil25, filIdx := pickNextFilament(rng, filIdx)
	body25 := fmt.Sprintf("%s • %s", jobName, fil25.material)
	fmt.Printf("  -> [T+15s] [25%%] Filament change! Updating Live Activity tile (ETA: %ds, Filament: %s, Color: %s)...\n", e25, fil25.material, fil25.color)
	tile25 := notify.TileContent{
		Title:    printerName,
		Body:     body25,
		Status:   "Printing",
		Symbol:   "printer.fill",
		Color:    fil25.color,
		Tint:     notify.FormatTint(fil25.color),
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

	// T = 30s: 50% (Filament Change #2)
	p50 := 50
	e50 := int((2 * r.interval).Seconds())
	fil50, filIdx := pickNextFilament(rng, filIdx)
	body50 := fmt.Sprintf("%s • %s", jobName, fil50.material)
	fmt.Printf("  -> [T+30s] [50%%] Filament change! Updating Live Activity tile (ETA: %ds, Filament: %s, Color: %s)...\n", e50, fil50.material, fil50.color)
	tile50 := notify.TileContent{
		Title:    printerName,
		Body:     body50,
		Status:   "Printing",
		Symbol:   "printer.fill",
		Color:    fil50.color,
		Tint:     notify.FormatTint(fil50.color),
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

	// T = 45s: 75% (Filament Change #3)
	p75 := 75
	e75 := int(r.interval.Seconds())
	fil75, _ := pickNextFilament(rng, filIdx)
	body75 := fmt.Sprintf("%s • %s", jobName, fil75.material)
	fmt.Printf("  -> [T+45s] [75%%] Filament change! Updating Live Activity tile (ETA: %ds, Filament: %s, Color: %s)...\n", e75, fil75.material, fil75.color)
	tile75 := notify.TileContent{
		Title:    printerName,
		Body:     body75,
		Status:   "Printing",
		Symbol:   "printer.fill",
		Color:    fil75.color,
		Tint:     notify.FormatTint(fil75.color),
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
		Body:     body75,
		Status:   "Done",
		Symbol:   "checkmark.circle.fill",
		Color:    "green",
		Tint:     notify.FormatTint("green"),
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
