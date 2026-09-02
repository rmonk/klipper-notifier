package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/printer-notifier/notify-klipper/internal/config"
	"github.com/printer-notifier/notify-klipper/internal/coordinator"
	"github.com/printer-notifier/notify-klipper/internal/moonraker"
	"github.com/printer-notifier/notify-klipper/internal/notify"
	"github.com/printer-notifier/notify-klipper/internal/testmode"
)

var (
	Version = "0.1.0"
	Commit  = "unknown"
	Date    = "unknown"
)

func main() {
	var (
		configPath  string
		testMode    bool
		dryRun      bool
		verbose     bool
		showVersion bool
	)

	flag.StringVar(&configPath, "config", "", "Path to configuration file (.yaml or .env)")
	flag.StringVar(&configPath, "c", "", "Path to configuration file (.yaml or .env) (shorthand)")
	flag.BoolVar(&testMode, "test", false, "Run test mode: validates keys and sends a 1-minute simulated print job")
	flag.BoolVar(&testMode, "t", false, "Run test mode (shorthand)")
	flag.BoolVar(&dryRun, "dry-run", false, "Simulate Notify! API calls without contacting gateway")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose debug logging")
	flag.BoolVar(&verbose, "v", false, "Enable verbose debug logging (shorthand)")
	flag.BoolVar(&showVersion, "version", false, "Show version and exit")
	flag.Parse()

	if showVersion {
		fmt.Printf("notify-klipper v%s (commit: %s, built: %s)\n", Version, Commit, Date)
		os.Exit(0)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("[Error] Failed to load configuration: %v", err)
	}

	// CLI flags override config file / env
	if testMode {
		cfg.TestMode = true
	}
	if dryRun {
		cfg.DryRun = true
	}
	if verbose {
		cfg.Verbose = true
	}

	// Validate config
	if err := cfg.Validate(!cfg.DryRun); err != nil {
		log.Fatalf("[Error] Configuration error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Printf("\nReceived signal %s, shutting down...", sig)
		cancel()
	}()

	moonClient, err := moonraker.NewClient(cfg.MoonrakerURL, cfg.MoonrakerAPIKey)
	if err != nil {
		log.Fatalf("[Error] Failed to initialize Moonraker client: %v", err)
	}

	notifClient := notify.NewClient(
		cfg.NotifyBaseURL,
		cfg.NotifyDeviceID,
		cfg.NotifyDeviceToken,
		cfg.NotifyIconURL,
		cfg.DryRun,
	)

	if cfg.TestMode {
		runner := testmode.NewRunner(cfg, moonClient, notifClient)
		if err := runner.Run(ctx); err != nil {
			log.Fatalf("[Error] Test mode failed: %v", err)
		}
		os.Exit(0)
	}

	// Normal daemon mode
	log.Printf("Starting notify-klipper v%s for printer %q...", Version, cfg.PrinterName)
	log.Printf("  Moonraker URL: %s", cfg.MoonrakerURL)
	if cfg.MoonrakerAPIKey != "" {
		log.Printf("  Moonraker Auth: API Key configured")
	} else {
		log.Printf("  Moonraker Auth: None (assumed unauthenticated / trusted LAN)")
	}
	log.Printf("  Notify Device: %s", cfg.NotifyDeviceID)
	if cfg.DryRun {
		log.Printf("  Mode: DRY-RUN (simulating Live Activity calls)")
	}

	// Verify Notify! credentials at startup
	if !cfg.DryRun {
		log.Printf("Verifying Notify! credentials...")
		linkResp, err := notifClient.Link(ctx)
		if err != nil {
			log.Fatalf("[Error] Notify! credential check failed: %v", err)
		}
		log.Printf("Connected to Notify! (device: %q, type: %s)", linkResp.Name, linkResp.Type)
	}

	// Launch Moonraker WebSocket client in background
	go moonClient.StartWebSocket(ctx)

	// Launch coordinator
	coord := coordinator.NewCoordinator(cfg, moonClient, notifClient)
	coord.Start(ctx)
	log.Println("notify-klipper stopped.")
}
