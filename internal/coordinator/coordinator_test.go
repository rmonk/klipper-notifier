package coordinator

import (
	"context"
	"testing"

	"github.com/printer-notifier/notify-klipper/internal/config"
	"github.com/printer-notifier/notify-klipper/internal/moonraker"
	"github.com/printer-notifier/notify-klipper/internal/notify"
)

func TestCleanJobName(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"voron_cube.gcode", "voron_cube"},
		{"/data/Metadata/benchy.gcode.3mf", "benchy"},
		{"sample.stl", "sample"},
		{"", "Untitled Print"},
	}

	for _, c := range cases {
		got := cleanJobName(c.input)
		if got != c.expected {
			t.Errorf("cleanJobName(%q) = %q, expected %q", c.input, got, c.expected)
		}
	}
}

func TestCalculateEndsIn(t *testing.T) {
	st := moonraker.MoonrakerStatus{
		EstimatedTime: 3600,
		PrintDuration: 600,
	}
	endsIn := calculateEndsIn(st)
	if endsIn == nil || *endsIn != 3000 {
		t.Fatalf("expected endsIn 3000, got %v", endsIn)
	}

	// Out of bounds ceiling
	st.EstimatedTime = 200000
	st.PrintDuration = 0
	endsIn = calculateEndsIn(st)
	if endsIn != nil {
		t.Fatalf("expected nil for ETA > 86400, got %v", *endsIn)
	}
}

func TestCoordinatorTransitions(t *testing.T) {
	cfg := config.NewDefault()
	cfg.PrinterName = "TestPrinter"
	notifCli := notify.NewClient("https://push.getnotifyapp.com", "dev1", "tok1", "", true) // dry-run
	moonCli, _ := moonraker.NewClient("http://127.0.0.1:7125", "")

	coord := NewCoordinator(cfg, moonCli, notifCli)
	ctx := context.Background()

	// 1. Transition to Printing
	coord.HandleStatus(ctx, moonraker.MoonrakerStatus{
		PrintState:    "printing",
		Filename:      "benchy.gcode",
		Progress:      0.10,
		PrintDuration: 60,
		EstimatedTime: 600,
		TotalLayers:   100,
		CurrentLayer:  10,
		ExtruderTemp:  210,
		BedTemp:       60,
	})

	if coord.activityID == "" {
		t.Fatal("expected active activityID after start, got empty")
	}

	// 2. Transition to Paused
	coord.HandleStatus(ctx, moonraker.MoonrakerStatus{
		PrintState: "paused",
		Filename:   "benchy.gcode",
		Progress:   0.50,
	})
	if !coord.isPaused {
		t.Fatal("expected coordinator to be in paused state")
	}

	// 3. Complete
	coord.HandleStatus(ctx, moonraker.MoonrakerStatus{
		PrintState:    "complete",
		Filename:      "benchy.gcode",
		Progress:      1.0,
		PrintDuration: 600,
	})
	if coord.activityID != "" {
		t.Fatalf("expected activityID to be cleared after complete, got %s", coord.activityID)
	}
}
