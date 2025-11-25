package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"

	"aws-relay/internal/store"
)

type Dashboard struct {
	store *store.Store
	mux   *http.ServeMux
}

func New(s *store.Store) *Dashboard {
	d := &Dashboard{
		store: s,
		mux:   http.NewServeMux(),
	}

	d.mux.HandleFunc("/", d.handleIndex)
	d.mux.HandleFunc("/api/stats", d.handleStats)
	d.mux.HandleFunc("/api/messages", d.handleMessages)
	d.mux.HandleFunc("/api/history", d.handleHistory)
	d.mux.HandleFunc("/api/clear", d.handleClear)

	return d
}

func (d *Dashboard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS headers for local development
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	d.mux.ServeHTTP(w, r)
}

func (d *Dashboard) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(indexHTML))
}

func (d *Dashboard) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := d.store.GetQueueStats()
	writeJSON(w, stats)
}

func (d *Dashboard) handleMessages(w http.ResponseWriter, r *http.Request) {
	queueName := r.URL.Query().Get("queue")
	includeDeleted := r.URL.Query().Get("deleted") == "true"

	messages := d.store.GetMessages(queueName, includeDeleted)
	if messages == nil {
		messages = []*store.Message{}
	}
	writeJSON(w, messages)
}

func (d *Dashboard) handleHistory(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	history := d.store.GetHistory(limit)
	if history == nil {
		history = []*store.Message{}
	}
	writeJSON(w, history)
}

func (d *Dashboard) handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" && r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	d.store.Clear()
	writeJSON(w, map[string]string{"status": "cleared"})
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>AWS Relay Dashboard</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #1a1a2e;
            color: #eee;
            padding: 20px;
        }
        h1 { color: #00d9ff; margin-bottom: 20px; }
        h2 { color: #00d9ff; margin: 20px 0 10px; font-size: 1.2em; }
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
            gap: 15px;
            margin-bottom: 20px;
        }
        .stat-card {
            background: #16213e;
            border-radius: 8px;
            padding: 15px;
            border-left: 4px solid #00d9ff;
        }
        .stat-card h3 {
            color: #00d9ff;
            font-size: 1em;
            margin-bottom: 10px;
            word-break: break-all;
        }
        .stat-numbers {
            display: grid;
            grid-template-columns: repeat(4, 1fr);
            gap: 10px;
            text-align: center;
        }
        .stat-numbers div { font-size: 0.8em; color: #888; }
        .stat-numbers span { display: block; font-size: 1.5em; font-weight: bold; }
        .sent span { color: #4ade80; }
        .received span { color: #60a5fa; }
        .deleted span { color: #f87171; }
        .pending span { color: #fbbf24; }
        .controls {
            margin-bottom: 20px;
            display: flex;
            gap: 10px;
            flex-wrap: wrap;
            align-items: center;
        }
        button, select {
            background: #16213e;
            border: 1px solid #00d9ff;
            color: #00d9ff;
            padding: 8px 16px;
            border-radius: 4px;
            cursor: pointer;
            font-size: 0.9em;
        }
        button:hover { background: #00d9ff; color: #1a1a2e; }
        select { background: #16213e; }
        label { display: flex; align-items: center; gap: 5px; color: #888; }
        input[type="checkbox"] { accent-color: #00d9ff; }
        .history-list {
            background: #16213e;
            border-radius: 8px;
            max-height: 600px;
            overflow-y: auto;
        }
        .history-item {
            padding: 12px 15px;
            border-bottom: 1px solid #1a1a2e;
            cursor: pointer;
        }
        .history-item:hover { background: #1a1a2e; }
        .history-item:last-child { border-bottom: none; }
        .history-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 5px;
        }
        .action-badge {
            padding: 2px 8px;
            border-radius: 4px;
            font-size: 0.75em;
            font-weight: bold;
        }
        .action-send { background: #4ade80; color: #000; }
        .action-receive { background: #60a5fa; color: #000; }
        .action-delete { background: #f87171; color: #000; }
        .queue-name { color: #888; font-size: 0.85em; }
        .timestamp { color: #666; font-size: 0.8em; }
        .message-id { color: #888; font-size: 0.8em; font-family: monospace; }
        .message-body {
            margin-top: 8px;
            padding: 10px;
            background: #0f0f1a;
            border-radius: 4px;
            font-family: monospace;
            font-size: 0.85em;
            white-space: pre-wrap;
            word-break: break-all;
            max-height: 200px;
            overflow-y: auto;
            display: none;
        }
        .history-item.expanded .message-body { display: block; }
        .no-data {
            padding: 40px;
            text-align: center;
            color: #666;
        }
        .refresh-indicator {
            color: #666;
            font-size: 0.8em;
        }
    </style>
</head>
<body>
    <h1>AWS Relay Dashboard</h1>

    <h2>Queue Statistics</h2>
    <div id="stats" class="stats-grid">
        <div class="no-data">Loading...</div>
    </div>

    <h2>Message History</h2>
    <div class="controls">
        <button onclick="refreshData()">Refresh</button>
        <button onclick="clearData()">Clear All</button>
        <select id="queueFilter" onchange="refreshData()">
            <option value="">All Queues</option>
        </select>
        <label>
            <input type="checkbox" id="showDeleted" onchange="refreshData()"> Show deleted
        </label>
        <label>
            <input type="checkbox" id="autoRefresh" onchange="toggleAutoRefresh()"> Auto-refresh
        </label>
        <span class="refresh-indicator" id="refreshIndicator"></span>
    </div>
    <div id="history" class="history-list">
        <div class="no-data">No messages yet</div>
    </div>

    <script>
        let autoRefreshInterval = null;
        let knownQueues = new Set();

        async function fetchJSON(url) {
            const res = await fetch(url);
            return res.json();
        }

        async function refreshData() {
            await Promise.all([refreshStats(), refreshHistory()]);
            document.getElementById('refreshIndicator').textContent =
                'Last updated: ' + new Date().toLocaleTimeString();
        }

        async function refreshStats() {
            const stats = await fetchJSON('/api/stats');
            const container = document.getElementById('stats');

            if (!stats || stats.length === 0) {
                container.innerHTML = '<div class="no-data">No queue activity yet</div>';
                return;
            }

            // Update queue filter
            const filter = document.getElementById('queueFilter');
            stats.forEach(s => {
                if (!knownQueues.has(s.queueName)) {
                    knownQueues.add(s.queueName);
                    const opt = document.createElement('option');
                    opt.value = s.queueName;
                    opt.textContent = s.queueName;
                    filter.appendChild(opt);
                }
            });

            container.innerHTML = stats.map(s => ` + "`" + `
                <div class="stat-card">
                    <h3>${s.queueName}</h3>
                    <div class="stat-numbers">
                        <div class="sent"><span>${s.totalSent}</span>Sent</div>
                        <div class="received"><span>${s.totalReceived}</span>Received</div>
                        <div class="deleted"><span>${s.totalDeleted}</span>Deleted</div>
                        <div class="pending"><span>${s.pending}</span>Pending</div>
                    </div>
                </div>
            ` + "`" + `).join('');
        }

        async function refreshHistory() {
            const queue = document.getElementById('queueFilter').value;
            const includeDeleted = document.getElementById('showDeleted').checked;

            let url = '/api/history?limit=200';
            const history = await fetchJSON(url);
            const container = document.getElementById('history');

            if (!history || history.length === 0) {
                container.innerHTML = '<div class="no-data">No messages yet</div>';
                return;
            }

            const filtered = history.filter(m => {
                if (queue && m.queueName !== queue) return false;
                if (!includeDeleted && m.action === 'delete') return false;
                return true;
            });

            if (filtered.length === 0) {
                container.innerHTML = '<div class="no-data">No matching messages</div>';
                return;
            }

            container.innerHTML = filtered.map(m => {
                const time = new Date(m.timestamp).toLocaleTimeString();
                const bodyPreview = m.body ? formatBody(m.body) : '[no body]';
                return ` + "`" + `
                    <div class="history-item" onclick="this.classList.toggle('expanded')">
                        <div class="history-header">
                            <span class="action-badge action-${m.action}">${m.action.toUpperCase()}</span>
                            <span class="queue-name">${m.queueName}</span>
                            <span class="timestamp">${time}</span>
                        </div>
                        <div class="message-id">${m.messageId || m.receiptHandle?.substring(0, 50) + '...' || 'N/A'}</div>
                        <div class="message-body">${bodyPreview}</div>
                    </div>
                ` + "`" + `;
            }).join('');
        }

        function formatBody(body) {
            try {
                const parsed = JSON.parse(body);
                return JSON.stringify(parsed, null, 2);
            } catch {
                return body;
            }
        }

        async function clearData() {
            if (confirm('Clear all captured messages?')) {
                await fetch('/api/clear', { method: 'POST' });
                knownQueues.clear();
                document.getElementById('queueFilter').innerHTML = '<option value="">All Queues</option>';
                refreshData();
            }
        }

        function toggleAutoRefresh() {
            if (document.getElementById('autoRefresh').checked) {
                autoRefreshInterval = setInterval(refreshData, 2000);
            } else {
                clearInterval(autoRefreshInterval);
            }
        }

        // Initial load
        refreshData();
    </script>
</body>
</html>`
