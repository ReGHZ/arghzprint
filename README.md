# arghzprint

Local print daemon. Receives print jobs from any backend via WebSocket, renders
them using configurable Handlebars templates, and sends to OS-managed printers.

Built for thermal receipt printers (80mm) but works with any printer the OS can see.

```
Backend (VPS)                   arghzprint (local PC)
─────────────────               ──────────────────────
Create PrintJob          →      Receive via WebSocket
Send { type, payload }  →      Render template + data
Track status             ←      Report DONE / FAILED
```

---

## Quick Start

**Download** a release binary for your OS from the [releases page](#).

**Run:**

```bash
./arghzprint          # Linux / Mac
arghzprint.exe        # Windows
```

Open **http://localhost:7878** in your browser → configure backend URL + printer.

On first run, `config.json` and default templates are created in:

- Windows: `%APPDATA%\arghzprint\`
- Linux: `~/.local/share/arghzprint/`
- Mac: `~/Library/Application Support/arghzprint/`

---

## How It Works

1. arghzprint connects to your backend via WebSocket and sends `printer.hello`
   to identify itself (agent ID, the job types it handles, version)
2. Backend pushes a print job: `{ event: "print.job", data: { jobId, type, payload } }`
3. arghzprint **claims** the job (`POST /api/printer/jobs/:id/claim`) before doing
   anything — if another agent already claimed it, this one skips it
4. Finds the template for that `type` (e.g. `kitchen.html`)
5. Renders the template with `payload` as the data context
6. Converts HTML → PDF via wkhtmltopdf
7. Sends PDF to the configured printer for that type
8. Reports status back to backend via `PATCH /api/printer/jobs/:id` (tagged with its agent ID)

If arghzprint was offline when jobs were created, it fetches all `PENDING` jobs
from the backend on reconnect — no jobs are lost.

### Running multiple agents

The claim step makes it safe to run several agents against the same backend
(e.g. one PC per station). Each agent has a stable, auto-generated **agent ID**,
announces which types it handles via `printer.hello`, and atomically claims a job
before printing — so two agents never double-print the same job.

---

## Protocol

Any backend that implements this protocol can use arghzprint.

### WebSocket

Connect: `ws://your-api/api/printer/ws`
Auth header: `Authorization: Bearer <printer_token>`

**arghzprint → Backend** (sent once, immediately after connect):

```json
{
  "event": "printer.hello",
  "data": {
    "agentId": "uuid...",
    "enabledTypes": ["KITCHEN", "BAR"],
    "version": "0.1.0"
  }
}
```

Use `enabledTypes` to dispatch each job only to agents that handle its type.

**Backend → arghzprint** (push job):

```json
{
  "event": "print.job",
  "data": {
    "jobId":   "cuid...",
    "type":    "KITCHEN",
    "payload": {}
  }
}
```

`priority` is optional. arghzprint uses its local `priority_map` config when set.
If not configured, falls back to the value sent by backend (defaults to 0).

Status updates are **not** sent over the WebSocket — they go via `PATCH` (below), so
the only daemon → backend WS messages are `printer.hello` and `pong`.

### HTTP Endpoints (called by arghzprint → your backend)

```
GET   /api/printer/jobs/pending    → { jobs: [ JobEnvelope ] }
POST  /api/printer/jobs/:id/claim  → { agentId }            ⇒ { claimed: bool }
PATCH /api/printer/jobs/:id        → { agentId, status, error? }
```

- **claim** is called before a job is printed. Return `{ "claimed": true }` for the
  first agent to ask and `{ "claimed": false }` (or HTTP `409`) for everyone after —
  this is what prevents two agents from printing the same job.
- **pending** is called on startup and reconnect to recover jobs that arrived while
  offline. Each recovered job is claimed before printing, same as a live push.
- **PATCH** carries the `agentId` so you can verify the update comes from the agent
  that actually owns the job.

### Job Status Lifecycle

```
PENDING → DISPATCHED → (claim) → ACKNOWLEDGED → PRINTING → COMPLETED
                                                         ↘ FAILED (retried up to 3×)
```

`ACKNOWLEDGED` is sent (via `PATCH`) right after a successful claim — for every source,
WebSocket push and pending recovery alike — before the worker moves the job to `PRINTING`.

A job whose type this agent doesn't have enabled is left untouched: the agent never
claims it or changes its status, so the backend can route it to an agent that does
(and `hello.enabledTypes` lets the backend avoid dispatching it here in the first place).

### Priority

Configured in arghzprint Settings (`priority_map`) — not by the backend.
Higher number = processed first. FIFO within the same priority.

Default values (set in arghzprint config, editable):

| Type     | Default |
| -------- | ------- |
| KITCHEN  | 2       |
| BAR      | 1       |
| CUSTOMER | 0       |
| BILLIARD | 0       |

If `priority_map` has no entry for a type, the backend's value is used (typically 0).
Backends that manage priority themselves can skip `priority_map` entirely.

---

## Template System

Templates are **Handlebars** (`.html`) files stored in the data directory.
Each job `type` maps to a file: `KITCHEN` → `kitchen.html`, `INVOICE` → `invoice.html`.

Edit templates at **http://localhost:7878/templates** — live preview updates as you type.

### Variables

Template variables come directly from the `payload` field of the print job.
Whatever keys the backend sends are available in the template.

**Example:**

```json
{
  "type": "KITCHEN",
  "payload": {
    "orderId": 42,
    "tableLabels": ["A1", "A2"],
    "items": [{ "name": "Nasi Goreng", "qty": 2, "notes": "Extra pedas" }]
  }
}
```

```html
<p>Order #{{orderId}}</p>
<p>Table: {{tableLabels}}</p>
{{#each items}}
<div>{{qty}}x {{name}} {{#if notes}}— {{notes}}{{/if}}</div>
{{/each}}
```

### Handlebars Quick Reference

```handlebars
{{value}}
output a value
{{#if value}}...{{/if}}
conditional
{{#unless value}}...{{/unless}}
{{#each array}}...{{/each}}
loop —
{{this}}
is current item
{{#each array}}
  {{@index}}
  {{@first}}
  {{@last}}
  loop metadata
{{/each}}
```

---

## Default Template Payload Reference

These are the payload keys sent by the **wulfcafe** backend integration.
If you're building your own backend, send whatever keys your template needs.

### KITCHEN / BAR

```
orderId        number    order ID
orderToken     string    short display token (e.g. "A3")
tableLabels    string[]  table names
customerName   string
serviceType    string    "DINE_IN" | "TAKEAWAY" | "DELIVERY"
station        string    "KITCHEN" | "BAR" | "MIXED"
notes          string    order-level notes
timestamp      string    formatted time string

items[]
  name         string
  qty          number
  notes        string    per-item note
  station      string    "KITCHEN" | "BAR"
  servingType  string    "HOT" | "ICE"
  bundleLabel  string    bundle/set label if applicable
  modifiers[]
    name       string
    qty        number
```

### CUSTOMER

```
orderId        number
orderToken     string
tableLabels    string[]
customerName   string
customerPhone  string
serviceType    string
notes          string
timestamp      string

items[]
  name         string
  qty          number
  unitPrice    string    formatted price
  totalPrice   string
  notes        string
  servingType  string
  modifiers[]
    name       string

subtotal       string    formatted
discount       string
tax            string
serviceCharge  string
total          string

wifiSsid       string    included if WiFi info is configured
wifiPassword   string
```

### BILLIARD

```
bookingCode    string    e.g. "BIL-001"
tableLabel     string
customerName   string
customerPhone  string
personCount    number
pricePerSession string   formatted
paymentMethod  string
timestamp      string

slots[]
  sessionLabel string
  date         string
  startTime    string
  endTime      string

subtotal       string
discount       string
tax            string
serviceCharge  string
total          string
```

---

## Configuration

`config.json` — edited via the Settings page or manually:

```json
{
  "agent_id": "auto-generated on first run — do not edit",
  "backend_url": "https://api.example.com",
  "printer_token": "secret",
  "ws_path": "/api/printer/ws",
  "polling_interval_seconds": 5,
  "web_ui_port": 7878,
  "printer_map": {
    "KITCHEN": "Epson-TM-T82-Dapur",
    "BAR": "Epson-TM-T82-Dapur",
    "CUSTOMER": "Epson-Kasir",
    "BILLIARD": "Epson-Kasir"
  },
  "enabled_types": {
    "KITCHEN": true,
    "BAR": true,
    "CUSTOMER": true,
    "BILLIARD": false
  },
  "max_job_age_minutes": 30,
  "network_printers": [
    { "name": "Dapur", "host": "192.168.1.50", "port": 9100, "width_dots": 576 }
  ]
}
```

`agent_id` is generated automatically on first run and shown (read-only) on the
Settings page. It's how the backend tells your agents apart — don't edit or copy it
between machines.

`printer_map` and `enabled_types` keys are managed via the Settings page — type names
are always derived from existing templates, never entered manually.

`enabled_types` is an **allowlist**: only types set to `true` are printed. A job for a
type that's `false` (or not listed) is simply ignored — this agent won't claim it or
touch its status, leaving it for an agent that does handle it. This same list is sent
to the backend in `printer.hello`, so the backend can avoid dispatching unwanted types
in the first place.

Jobs are kept in `arghzprint.db` (SQLite, next to `config.json`) from claim until the
backend has their final status, so a crash or restart resumes them instead of losing
them. Each printer gets its own worker: while a printer is unreachable its jobs retry
with backoff (5s → 60s) for up to `max_job_age_minutes`, then go `FAILED` — other
printers keep printing meanwhile.

`network_printers` is only used on Android (see below).

**Finding your printer name:**

The Settings page lists installed printers automatically in the printer name field
suggestions (detected via `Get-Printer` on Windows, `lpstat -a` on Linux/Mac).
You can also type a name manually if you know it.

---

## Custom Job Types

You're not limited to KITCHEN / BAR / CUSTOMER / BILLIARD.

1. Go to **Templates → + New**, enter a type name (e.g. `INVOICE`)
2. Edit the template — use `{{anyKey}}` for whatever your backend sends
3. Open **Settings** — `INVOICE` now appears automatically in printer mapping
4. Assign a printer and toggle it on
5. Backend sends `{ "type": "INVOICE", "payload": { "invoiceId": 1, ... } }`

Templates are the source of truth for job types. Printer mapping in Settings
is always derived from existing templates — no manual type entry, no typo risk.
To remove a type: delete its template, it disappears from Settings automatically.

---

## Deployment

### Windows (recommended)

**Option A — Task Scheduler (recommended for always-on machines)**

Runs at system boot before login. No third-party tools needed.

```bat
schtasks /create /tn "arghzprint" /tr "C:\path\to\arghzprint.exe" /sc ONSTART /ru SYSTEM /f
```

To stop and remove:

```bat
schtasks /end /tn "arghzprint"
schtasks /delete /tn "arghzprint" /f
```

**Option B — Startup folder (runs after user login)**

Place a shortcut to `arghzprint.exe` in:

```
%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup
```

**Option C — Run manually**

For testing or ad-hoc use, just double-click or run `arghzprint.exe` directly.
Web UI available at http://localhost:7878.

### Linux (systemd)

```ini
# /etc/systemd/system/arghzprint.service
[Unit]
Description=arghzprint local print daemon
After=network.target

[Service]
ExecStart=/usr/local/bin/arghzprint
Restart=on-failure
RestartSec=5
User=your-user

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now arghzprint
```

### Mac (launchd)

```xml
<!-- ~/Library/LaunchAgents/com.reghz.arghzprint.plist -->
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>        <string>com.reghz.arghzprint</string>
  <key>ProgramArguments</key>
  <array><string>/usr/local/bin/arghzprint</string></array>
  <key>RunAtLoad</key>    <true/>
  <key>KeepAlive</key>    <true/>
</dict>
</plist>
```

```bash
launchctl load ~/Library/LaunchAgents/com.reghz.arghzprint.plist
```

---

## Android

Same Go core, built with gomobile into an APK. Prints to any ESC/POS LAN printer over
raw TCP (port 9100) — no OS driver.

**Build** (CLI only — Android cmdline-tools with SDK 35 + build-tools 35 + NDK r28+,
JDK 17, `gomobile init` done; no Android Studio needed):

```bash
./android/build-aar.sh                 # Go core → android/app/libs/arghzprint.aar
cd android && JAVA_HOME=/usr/lib/jvm/java-17-openjdk ./gradlew assembleRelease
adb install -r app/build/outputs/apk/release/app-release.apk
```

`android/local.properties` needs `sdk.dir=<path to Android SDK>` (not committed).

**Install & set up the tablet**

1. Install the APK (allow "install unknown apps").
2. Open arghzprint → follow the Setup checklist (notifications, **battery optimization
   off** — required, app settings).
3. Settings → Backend URL + Printer Token (the location's `printerToken`).
4. Settings → Network printers → add each printer (name, IP from its self-test print,
   port 9100, width 576 for 80mm / 384 for 58mm).
5. Printer mapping → e.g. KITCHEN → Dapur, BAR + CUSTOMER → Bar → Save → Test.

Keep `web_ui_port` at 7878 on Android. Templates can be edited in the app, or edited on
a PC and brought in with **Import** (and taken out with **Export**).

Check receipts without a printer: run `go run ./cmd/fakeprinter -out ./fakeprints` on a
PC in the same network and add a network printer pointing to that PC's IP — every job is
saved as a PNG.

---

## Building from Source

Requirements: Go 1.21+, wkhtmltopdf binary

```bash
git clone https://github.com/ReGHZ/arghzprint
cd arghzprint

# place tool binaries (see below)
cp /path/to/wkhtmltopdf      tools/unix/wkhtmltopdf
cp /path/to/wkhtmltopdf.exe  tools/windows/wkhtmltopdf.exe
cp /path/to/SumatraPDF.exe   tools/windows/SumatraPDF.exe

# build
go build -o arghzprint ./cmd/arghzprint

# cross-compile for Windows from Linux
GOOS=windows GOARCH=amd64 go build -o arghzprint.exe ./cmd/arghzprint
```

**wkhtmltopdf** download: https://wkhtmltopdf.org/downloads.html
**SumatraPDF** download: https://www.sumatrapdfreader.org/download-free-pdf-viewer

---
