const API = {
  async json(method, path, body) {
    const opts = { method, headers: {} };
    if (body !== undefined) {
      opts.body = JSON.stringify(body);
      opts.headers['Content-Type'] = 'application/json';
    }
    const r = await fetch('/api' + path, opts);
    if (!r.ok && r.status !== 204) throw new Error(`${method} ${path} → ${r.status}`);
    if (r.status === 204 || r.headers.get('content-length') === '0') return null;
    return r.json();
  },

  async text(method, path, body) {
    const opts = { method, headers: { 'Content-Type': 'text/html' }, body };
    const r = await fetch('/api' + path, opts);
    if (!r.ok && r.status !== 204) throw new Error(`${method} ${path} → ${r.status}`);
    return r.status === 204 ? null : r.text();
  },

  get:    (path)        => API.json('GET',    path),
  put:    (path, body)  => API.json('PUT',    path, body),
  del:    (path)        => API.json('DELETE', path),
  post:   (path, body)  => API.json('POST',   path, body),
  putText:(path, body)  => API.text('PUT',    path, body),
  getText:(path)        => fetch('/api' + path).then(r => r.ok ? r.text() : Promise.reject(r.status)),
};

// ─── Toast store ──────────────────────────────

document.addEventListener('alpine:init', () => {

  Alpine.store('toast', {
    items: [],
    show(msg, type = 'success') {
      const id = Date.now();
      this.items.push({ id, msg, type });
      setTimeout(() => { this.items = this.items.filter(t => t.id !== id); }, 3000);
    },
  });

  // ─── Nav ──────────────────────────────────

  Alpine.store('nav', {
    page: 'dashboard',
    go(p) { this.page = p; },
  });

  // ─── Status — shared, polled once for everyone ──────────
  // Sidebar footer, dashboard page, and jobs page all read from here.
  // No duplicate intervals.

  Alpine.store('status', {
    connected: false,
    queueDepth: 0,
    backendUrl: '',
    _timer: null,

    async start() {
      await this.load();
      this._timer = setInterval(() => this.load(), 5000);
    },

    async load() {
      try {
        const [s, cfg] = await Promise.all([API.get('/status'), API.get('/settings')]);
        this.connected  = s.connected;
        this.queueDepth = s.queueDepth ?? 0;
        this.backendUrl = cfg.backend_url ?? '';
      } catch {}
    },
  });

  // ─── Settings ─────────────────────────────

  Alpine.data('settings', () => ({
    cfg: {
      backend_url: '',
      printer_token: '',
      ws_path: '/api/printer/ws',
      polling_interval_seconds: 5,
      web_ui_port: 7878,
      connection_mode: 'websocket',
      max_retries: 3,
      printer_map: {},
      enabled_types: {},
      priority_map: {},
    },
    savedPort: null,
    needsRestart: false,
    printers: [],
    rows: [],
    loading: true,

    async init() {
      try {
        const [cfg, printersRes, tplRes] = await Promise.all([
          API.get('/settings'),
          API.get('/printers'),
          API.get('/templates'),
        ]);
        this.cfg        = cfg;
        this.savedPort  = cfg.web_ui_port;
        this.printers   = printersRes.printers ?? [];
        this.syncRows(tplRes.types ?? []);
      } finally {
        this.loading = false;
      }
    },

    syncRows(templateTypes) {
      this.rows = templateTypes.map(type => ({
        type,
        printer:  this.cfg.printer_map?.[type]  ?? '',
        enabled:  this.cfg.enabled_types?.[type] ?? true,
        priority: this.cfg.priority_map?.[type]  ?? 0,
      }));
    },

    async testPrint(row) {
      if (!row.printer) return;
      try {
        await API.post('/test-print', { type: row.type, printer: row.printer });
        Alpine.store('toast').show(`Test sent to ${row.printer}`);
      } catch (e) {
        Alpine.store('toast').show(e.message, 'error');
      }
    },

    async save() {
      const printer_map   = {};
      const enabled_types = {};
      const priority_map  = {};
      this.rows.forEach(r => {
        if (r.printer) printer_map[r.type] = r.printer;
        enabled_types[r.type] = r.enabled;
        if (r.priority !== 0) priority_map[r.type] = parseInt(r.priority, 10);
      });

      try {
        await API.put('/settings', { ...this.cfg, printer_map, enabled_types, priority_map });
        this.needsRestart = this.cfg.web_ui_port !== this.savedPort;
        Alpine.store('toast').show('Settings saved');
      } catch (e) {
        Alpine.store('toast').show(e.message, 'error');
      }
    },
  }));

  // ─── Template Editor ──────────────────────

  Alpine.data('templateEditor', () => ({
    types: [],
    active: null,
    editor: null,
    previewHtml: '',
    previewPending: false,
    debounceTimer: null,
    newTypeDialog: false,
    newTypeName: '',
    unsaved: false,

    async init() {
      await this.loadTypes();

      // init Ace after DOM settles
      requestAnimationFrame(() => {
        this.editor = ace.edit('ace-editor');
        this.editor.setTheme('ace/theme/one_dark');
        this.editor.session.setMode('ace/mode/html');
        this.editor.setOptions({
          fontSize: '13px',
          tabSize: 2,
          useSoftTabs: true,
          wrap: false,
          showPrintMargin: false,
          fontFamily: "'Cascade Code', 'Fira Code', 'Courier New', monospace",
        });
        this.editor.on('change', () => {
          this.unsaved = true;
          this.schedulePreview();
        });

        if (this.types.length > 0) this.openType(this.types[0]);
      });
    },

    destroy() { this.editor?.destroy(); },

    async loadTypes() {
      try {
        const res = await API.get('/templates');
        this.types = res.types ?? [];
      } catch {}
    },

    async openType(type) {
      if (this.unsaved && this.active && !confirm('Unsaved changes. Discard?')) return;
      this.active = type;
      this.unsaved = false;
      try {
        const html = await API.getText('/templates/' + type);
        this.editor?.setValue(html, -1);
        this.updatePreview();
      } catch (e) {
        Alpine.store('toast').show('Failed to load template', 'error');
      }
    },

    schedulePreview() {
      clearTimeout(this.debounceTimer);
      this.debounceTimer = setTimeout(() => this.updatePreview(), 400);
    },

    async updatePreview() {
      if (!this.editor) return;
      const src = this.editor.getValue();
      if (!src.trim()) { this.previewHtml = ''; return; }

      this.previewPending = true;
      try {
        const res = await fetch('/api/preview', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ template: src, data: sampleData() }),
        });
        this.previewHtml = res.ok ? await res.text() : '';
      } catch {
        this.previewHtml = '';
      } finally {
        this.previewPending = false;
      }
    },

    async save() {
      if (!this.active) return;
      const html = this.editor.getValue();
      try {
        await API.putText('/templates/' + this.active, html);
        this.unsaved = false;
        Alpine.store('toast').show('Saved ' + this.active);
      } catch (e) {
        Alpine.store('toast').show('Save failed', 'error');
      }
    },

    async deleteActive() {
      if (!this.active || !confirm('Delete template ' + this.active + '?')) return;
      try {
        await API.del('/templates/' + this.active);
        this.types = this.types.filter(t => t !== this.active);
        this.active = this.types[0] ?? null;
        if (this.active) this.openType(this.active);
        else this.editor?.setValue('', -1);
        Alpine.store('toast').show('Deleted');
      } catch (e) {
        Alpine.store('toast').show('Delete failed', 'error');
      }
    },

    async confirmNewType() {
      const type = this.newTypeName.toUpperCase().trim();
      if (!type) return;
      const stub = defaultStub(type);
      try {
        await API.putText('/templates/' + type, stub);
        await this.loadTypes();
        this.openType(type);
      } catch (e) {
        Alpine.store('toast').show('Failed to create', 'error');
      }
      this.newTypeDialog = false;
      this.newTypeName = '';
    },
  }));

  // ─── Jobs ──────────────────────────────────

  Alpine.data('jobs', () => ({
    records: [],

    async init() {
      await this.load();
    },

    async load() {
      try {
        const data = await API.get('/jobs');
        this.records = data.records ?? [];
      } catch {}
    },

    statusClass(s) {
      return {
        'badge-success': s === 'COMPLETED',
        'badge-danger':  s === 'FAILED',
        'badge-warn':    s === 'PRINTING',
        'badge-muted':   s === 'ACKNOWLEDGED' || s === 'PENDING',
        'badge-accent':  s === 'DISPATCHED',
      };
    },
  }));

});

// ─── Boot ──────────────────────────────────────
// Start shared status polling once Alpine is ready.

document.addEventListener('alpine:initialized', () => {
  Alpine.store('status').start();
});

// ─── Sample data for preview ──────────────────

function sampleData() {
  return {
    orderId: 42,
    orderToken: 'A3',
    tableLabels: ['A1', 'A2'],
    customerName: 'Sample Customer',
    customerPhone: '08123456789',
    serviceType: 'DINE_IN',
    station: 'KITCHEN',
    items: [
      { name: 'Nasi Goreng Special', qty: 2, notes: 'Extra pedas', station: 'KITCHEN', servingType: 'HOT', modifiers: [{ name: 'Level 3', qty: 1 }] },
      { name: 'Es Teh Manis', qty: 1, notes: '', station: 'BAR', servingType: 'ICE', modifiers: [] },
    ],
    subtotal: 'Rp 55.000',
    discount: '',
    tax: 'Rp 5.500',
    serviceCharge: '',
    total: 'Rp 60.500',
    notes: 'Tolong jangan terlalu lama',
    wifiSsid: 'WulfCafe_Guest',
    wifiPassword: 'wulfcafe123',
    slots: [
      { sessionLabel: 'Session 1', date: '2026-06-01', startTime: '10:00', endTime: '11:00' },
      { sessionLabel: 'Session 2', date: '2026-06-01', startTime: '11:00', endTime: '12:00' },
    ],
    bookingCode: 'BIL-001',
    tableLabel: 'Meja 3',
    personCount: 2,
    pricePerSession: 'Rp 30.000',
    paymentMethod: 'CASH',
    timestamp: new Date().toLocaleString('id-ID'),
  };
}

function defaultStub(type) {
  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <style>
    @page { size: 80mm auto; margin: 0; }
    * { margin: 0; padding: 0; box-sizing: border-box; }
    body { width: 80mm; font-family: 'Courier New', monospace; font-size: 12px; padding: 8px 10px; }
  </style>
</head>
<body>
  <h2 style="text-align:center;margin-bottom:8px">${type}</h2>
  <p>Order: #{{orderId}}</p>
  {{#each items}}
    <div>{{qty}}x {{name}}</div>
  {{/each}}
  <p style="text-align:center;margin-top:8px;font-size:10px">{{timestamp}}</p>
</body>
</html>`;
}
