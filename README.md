# notify-klipper

> Real-time iOS Live Activities (Dynamic Island & Lock Screen widgets) and push notifications for Klipper / Fluidd / Mainsail 3D printers via the [Notify!](https://getnotifyapp.com/) service and [Moonraker](https://moonraker.readthedocs.io/en/latest/) API.

> [!NOTE]
> This project is inspired by and based on the concepts in [notify-bambuddy](https://github.com/simplytoast1/notify-bambuddy) by [@simplytoast1](https://github.com/simplytoast1), but is a standalone implementation tailored for Klipper / Moonraker ecosystems rather than a direct fork.

`notify-klipper` connects directly to your 3D printer's Moonraker API via WebSocket and HTTP REST, tracking print progress, layer height, temperatures, and job status in real time. It broadcasts rich iOS 16.1+ Live Activities to your Apple devices with zero cloud dependencies on the printer side.

---

## Features

- **iOS Live Activities & Dynamic Island**: Real-time progress bar, live countdown ETA, current/total layer counter, nozzle & bed temperatures.
- **Push Alerts**: Instant notifications for print start, print completion, pause/filament runout, and print failure.
- **Single Static Binary**: Ultra-lightweight Go daemon compiled for `x86_64` (`amd64`) and `aarch64` (`arm64`) with zero runtime dependencies.
- **Container Ready**: Includes `Containerfile`, `Dockerfile`, and `compose.yaml` with hardened security settings (read-only filesystem, non-root user, dropped capabilities).
- **Moonraker WebSocket Subscriptions**: Instant state updates with automatic reconnection and HTTP polling fallback.
- **Flexible Authentication**: Moonraker API keys are fully optional (works seamlessly on local trusted LANs) and supports Docker/Kubernetes secret files (`_FILE` suffix).
- **Interactive Test Mode**: Built-in test runner (`--test`) validates your Moonraker and Notify credentials and executes a 1-minute simulated print job sending updates every 15 seconds.

---

## Quickstart

### 1. Get your Notify! Device Credentials

1. Install the **Notify!** app on your iPhone or iPad from the App Store ([getnotifyapp.com](https://getnotifyapp.com/)).
2. Open Notify! and navigate to **Settings** -> **API Keys**.
3. Note your **Device ID** and **Device Token**.

---

## Running as a Standalone Binary

`notify-klipper` distributes as a single standalone executable.

### 1. Download or Build the Binary

To compile for your current system or cross-compile:

```bash
# Build for host system
make build

# Or cross-compile for both x86_64 and aarch64
make build-all
```

Binaries are output to `dist/`:
- `dist/notify-klipper-linux-amd64` (x86_64)
- `dist/notify-klipper-linux-arm64` (aarch64 / Raspberry Pi)

### 2. Configure and Run

You can configure `notify-klipper` via environment variables or a `.env` / `notify-klipper.yaml` file:

```bash
# Set environment variables
export MOONRAKER_URL="http://192.168.1.50:7125"
export NOTIFY_DEVICE_ID="your_notify_device_id"
export NOTIFY_DEVICE_TOKEN="your_notify_device_token"
export PRINTER_NAME="Voron 2.4"

# Run test mode to verify setup
./bin/notify-klipper --test

# Run in background daemon mode
./bin/notify-klipper
```

### 3. Running as a systemd Service (Optional)

Create `/etc/systemd/system/notify-klipper.service`:

```ini
[Unit]
Description=Notify Klipper Service
After=network.target

[Service]
Type=simple
User=pi
ExecStart=/usr/local/bin/notify-klipper -config /etc/notify-klipper/notify-klipper.yaml
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

---

## Running with Docker / Podman Compose

### 1. Configure `.env`

Copy the template:

```bash
cp .env.example .env
```

Edit `.env`:

```dotenv
MOONRAKER_URL=http://192.168.1.50:7125
NOTIFY_DEVICE_ID=your_device_id
NOTIFY_DEVICE_TOKEN=your_device_token
PRINTER_NAME=Voron 2.4
```

### 2. Start the Container

```bash
docker compose up -d
```

Check logs:

```bash
docker compose logs -f
```

---

## Test Mode (`--test` / `-t`)

`notify-klipper` includes a test mode that validates your configuration end-to-end:

```bash
./bin/notify-klipper --test
```

### What Test Mode Does:

1. **Moonraker Check**: Connects to `MOONRAKER_URL`, verifies Moonraker status, and validates `MOONRAKER_API_KEY` (if set; if empty, verifies unauthenticated access).
2. **Notify! Check**: Queries `https://push.getnotifyapp.com/link` to verify Device ID and Token validity, ensuring it's an iOS Device ID and not a Group ID.
3. **Simulated 1-Minute Print**:
   - **T = 0s**: Starts a Live Activity tile (`0%`, status: `Starting`, ETA: `60s`) and sends a `Print Started` push notification.
   - **T = 15s**: Updates Live Activity to `25%` (ETA: `45s`, Layer `25/100`).
   - **T = 30s**: Updates Live Activity to `50%` (ETA: `30s`, Layer `50/100`).
   - **T = 45s**: Updates Live Activity to `75%` (ETA: `15s`, Layer `75/100`).
   - **T = 60s**: Finishes print at `100%` (status: `Done`, color: `green`), sends a `Print Complete` push notification, closes the tile, and exits cleanly.

---

## Configuration Reference

Configuration can be provided through **Environment Variables**, **YAML File**, or **Secret Files**.

| Variable | Config File Key | Default | Description |
| :--- | :--- | :--- | :--- |
| `MOONRAKER_URL` | `moonraker_url` | `http://127.0.0.1:7125` | Base URL of Moonraker REST and WebSocket API. |
| `MOONRAKER_API_KEY` | `moonraker_api_key` | _(empty)_ | Moonraker API Key. Optional if Moonraker allows trusted LAN access. |
| `MOONRAKER_API_KEY_FILE` | - | _(empty)_ | Path to file containing Moonraker API Key. |
| `NOTIFY_DEVICE_ID` | `notify_device_id` | _(required)_ | Notify! Device ID for target iOS device. |
| `NOTIFY_DEVICE_ID_FILE` | - | _(empty)_ | Path to file containing Notify! Device ID. |
| `NOTIFY_DEVICE_TOKEN` | `notify_device_token` | _(required)_ | Notify! Device Token for target iOS device. |
| `NOTIFY_DEVICE_TOKEN_FILE` | - | _(empty)_ | Path to file containing Notify! Device Token. |
| `PRINTER_NAME` | `printer_name` | `Klipper` | Friendly printer display name (shown as widget title). |
| `SHOW_METRICS` | `show_metrics` | `false` | Enable extra metric badges (Nozzle & Bed temp, Layer count). |
| `SHOW_TRAILING_LAYER` | `show_trailing_layer` | `false` | Enable trailing layer text (e.g. `Layer 12/150`). |
| `ENABLE_PUSH_NOTIFICATIONS` | `enable_push_notifications` | `false` | Send separate push notifications on start/end/pause (default: false, only Live Activity is sent). |
| `POLL_INTERVAL` | `poll_interval` | `2s` | Fallback HTTP polling interval. |
| `KEEP_FOR_SECONDS` | `keep_for_seconds` | `300` | Seconds to keep completed tile on Lock Screen. |
| `DRY_RUN` | `dry_run` | `false` | When `true`, logs Notify! calls without contacting gateway. |
| `VERBOSE` | `verbose` | `false` | Enable verbose debug logging. |

### YAML Configuration Example (`notify-klipper.yaml`)

```yaml
moonraker_url: "http://192.168.1.50:7125"
moonraker_api_key: ""  # Optional if trusted LAN
notify_device_id: "your_device_id"
notify_device_token: "your_device_token"
printer_name: "Voron 2.4"
show_metrics: false
show_trailing_layer: false
enable_push_notifications: false
poll_interval: 2s
keep_for_seconds: 300
dry_run: false
verbose: false
```

---

## Moonraker API Key Setup

By default, Moonraker configures `[authorization]` in `moonraker.conf` allowing trusted clients from local IP ranges (e.g. `127.0.0.1`, `192.168.0.0/16`, `10.0.0.0/8`) without requiring an API key.

- If your `notify-klipper` instance is running on the same network or host, you can **leave `MOONRAKER_API_KEY` empty**.
- If your Moonraker instance requires authentication, generate an API key in **Fluidd** or **Mainsail** under **Settings** -> **API Keys** (or via `moonraker.conf`), and set `MOONRAKER_API_KEY=<your-key>`.

---

## Building from Source

### Requirements
- Go 1.22+
- `make`
- Docker or Podman (optional, for containers)

```bash
# Run unit tests
make test

# Build host binary in bin/notify-klipper
make build

# Cross-compile for Linux (amd64, arm64) and macOS (arm64, amd64)
make build-all

# Build container image
make container
```

---

## License

MIT License.
