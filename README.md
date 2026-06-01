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

1. arghzprint connects to your backend via WebSocket
2. Backend pushes a print job: `{ event: "print.job", data: { jobId, type, payload } }`
3. arghzprint finds the template for that `type` (e.g. `kitchen.html`)
4. Renders the template with `payload` as the data context
5. Converts HTML → PDF via wkhtmltopdf
6. Sends PDF to the configured printer for that type
7. Reports status back to backend via `PATCH /api/printer/jobs/:id`

If arghzprint was offline when jobs were created, it fetches all `PENDING` jobs
from the backend on reconnect — no jobs are lost.

---

## Protocol

Any backend that implements this protocol can use arghzprint.

### WebSocket

Connect: `ws://your-api/api/printer/ws`
Auth header: `Authorization: Bearer <printer_token>`

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

**arghzprint → Backend** (status update):

```json
{
  "event": "print.ack",
  "data": {
    "jobId": "cuid...",
    "status": "ACKNOWLEDGED | PRINTING | COMPLETED | FAILED",
    "error": "only set on FAILED"
  }
}
```

### HTTP Endpoints (called by arghzprint → your backend)

```
GET  /api/printer/jobs/pending   → { jobs: [ JobEnvelope ] }
PATCH /api/printer/jobs/:id      → { status, error? }
```

Called on startup and reconnect to recover jobs that arrived while offline.

### Job Status Lifecycle

```
PENDING → DISPATCHED → ACKNOWLEDGED → PRINTING → COMPLETED
                                                ↘ FAILED (retried up to 3×)
```

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
  }
}
```

`printer_map` and `enabled_types` keys are managed via the Settings page — type names
are always derived from existing templates, never entered manually.

`enabled_types`: `false` disables auto-print for that type. Jobs are still acknowledged
but not printed — useful for types you want to handle differently per machine.

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
