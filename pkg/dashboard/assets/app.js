// State Management
let logBuffer = [];
const maxLogs = 150;
let isPaused = false;
let sseSource = null;
let statsInterval = null;

// DOM Elements
const logTerminal = document.getElementById('log-terminal');
const searchInput = document.getElementById('search-input');
const sourceFilter = document.getElementById('source-filter');
const codeFilter = document.getElementById('code-filter');
const pauseBtn = document.getElementById('pause-btn');
const clearBtn = document.getElementById('clear-btn');
const inspectorDrawer = document.getElementById('inspector-drawer');
const drawerOverlay = document.getElementById('drawer-overlay');
const closeInspectorBtn = document.getElementById('close-inspector-btn');

// Stats Elements
const totalLogsEl = document.getElementById('metric-total-logs');
const zosLogsEl = document.getElementById('metric-zos-logs');
const as400LogsEl = document.getElementById('metric-as400-logs');
const emsLogsEl = document.getElementById('metric-ems-logs');
const rateZosEl = document.getElementById('rate-zos');
const rateAs400El = document.getElementById('rate-as400');
const rateEmsEl = document.getElementById('rate-ems');
const cpuValueEl = document.getElementById('cpu-value');
const cpuGauge = document.getElementById('cpu-gauge');
const ramValueEl = document.getElementById('ram-value');
const ramGauge = document.getElementById('ram-gauge');
const uptimeEl = document.getElementById('agent-uptime');
const taxonomyBreakdown = document.getElementById('taxonomy-breakdown');

// Inspector Elements
const inspCode = document.getElementById('insp-code');
const inspDesc = document.getElementById('insp-desc');
const inspPlatform = document.getElementById('insp-platform');
const inspSeverity = document.getElementById('insp-severity');
const jsonCode = document.getElementById('json-code');

// Initialize Dashboard
document.addEventListener('DOMContentLoaded', () => {
    connectStream();
    startStatsPolling();
    setupEventListeners();
});

// Setup Control Event Listeners
function setupEventListeners() {
    pauseBtn.addEventListener('click', togglePause);
    clearBtn.addEventListener('click', clearTerminal);
    
    // Filters and Search
    searchInput.addEventListener('input', renderFilteredLogs);
    sourceFilter.addEventListener('change', renderFilteredLogs);
    codeFilter.addEventListener('change', renderFilteredLogs);

    // Inspector Drawer closing
    closeInspectorBtn.addEventListener('click', closeInspector);
    drawerOverlay.addEventListener('click', closeInspector);
}

// Connect SSE stream
function connectStream() {
    sseSource = new EventSource('/api/stream');

    sseSource.onopen = () => {
        logTerminal.innerHTML = '';
        console.log("SSE Telemetry channel established.");
    };

    sseSource.onerror = (e) => {
        console.error("SSE connection lost. Reconnecting in 3s...", e);
        sseSource.close();
        
        logTerminal.innerHTML = `
            <div class="terminal-placeholder">
                <div class="spinner"></div>
                <p>Connection lost. Attempting server reconnection...</p>
            </div>
        `;
        setTimeout(connectStream, 3000);
    };

    sseSource.onmessage = (event) => {
        if (isPaused) return;

        try {
            const logRecord = JSON.parse(event.data);
            processNewLog(logRecord);
        } catch (err) {
            console.error("Error parsing received event:", err);
        }
    };
}

// Process new log record into memory and render
function processNewLog(logRecord) {
    // Add to buffer
    logBuffer.unshift(logRecord);
    if (logBuffer.length > maxLogs) {
        logBuffer.pop();
    }

    renderLogItem(logRecord);
}

// Render single log item in terminal
function renderLogItem(log) {
    // Remove placeholder if present
    const placeholder = logTerminal.querySelector('.terminal-placeholder');
    if (placeholder) {
        placeholder.remove();
    }

    // Apply filtering
    if (!shouldShowLog(log)) return;

    const row = createLogRowElement(log);
    logTerminal.insertBefore(row, logTerminal.firstChild);

    // Maintain terminal limit in DOM
    const rows = logTerminal.querySelectorAll('.log-row');
    if (rows.length > maxLogs) {
        rows[rows.length - 1].remove();
    }
}

// Check if log satisfies active filters
function shouldShowLog(log) {
    const searchVal = searchInput.value.toLowerCase();
    const sourceVal = sourceFilter.value;
    const categoryVal = codeFilter.value;

    const code = log.attributes["legacy.user_code"] || "";
    const body = log.body || "";
    const desc = log.attributes["legacy.user_code_description"] || "";
    const platform = log.resource["os.type"] || "";

    // Search filter
    if (searchVal && 
        !body.toLowerCase().includes(searchVal) && 
        !code.toLowerCase().includes(searchVal) && 
        !desc.toLowerCase().includes(searchVal)) {
        return false;
    }

    // Source platform filter
    if (sourceVal !== 'all' && platform !== sourceVal) {
        return false;
    }

    // Category code filter
    if (categoryVal !== 'all' && !code.startsWith(categoryVal)) {
        return false;
    }

    return true;
}

// Create individual row DOM element
function createLogRowElement(log) {
    const row = document.createElement('div');
    row.className = 'log-row';
    
    // Add click details inspector
    row.addEventListener('click', () => openInspector(log));

    const timeStr = new Date(log.timestamp).toLocaleTimeString();
    const platform = log.resource["os.type"] || "sys";
    const code = log.attributes["legacy.user_code"] || "SYS";
    const desc = log.attributes["legacy.user_code_description"] || "System update";
    const body = log.body || "";

    let codeClass = "";
    if (code.startsWith("LL")) codeClass = "auth";
    if (code.startsWith("PA")) codeClass = "priv";

    row.innerHTML = `
        <span class="log-time">${timeStr}</span>
        <span class="log-platform ${platform}">${platform}</span>
        <span class="log-code ${codeClass}">${code}</span>
        <span class="log-desc">${desc}</span>
        <span class="log-body">${body}</span>
    `;

    return row;
}

// Filter and re-render the entire console buffer
function renderFilteredLogs() {
    logTerminal.innerHTML = '';
    const filtered = logBuffer.filter(shouldShowLog);
    
    if (filtered.length === 0) {
        logTerminal.innerHTML = `
            <div class="terminal-placeholder">
                <p>No events match the active search filters.</p>
            </div>
        `;
        return;
    }

    filtered.forEach(log => {
        const row = createLogRowElement(log);
        logTerminal.appendChild(row); // Append in order (newest first in pre-sorted buffer)
    });
}

// Toggle Stream Pause
function togglePause() {
    isPaused = !isPaused;
    if (isPaused) {
        pauseBtn.textContent = 'Resume Stream';
        pauseBtn.classList.remove('primary');
        pauseBtn.classList.add('secondary');
        console.log("Telemetry stream paused.");
    } else {
        pauseBtn.textContent = 'Pause Stream';
        pauseBtn.classList.remove('secondary');
        pauseBtn.classList.add('primary');
        console.log("Telemetry stream active.");
        // Flush screen
        renderFilteredLogs();
    }
}

// Clear log console
function clearTerminal() {
    logBuffer = [];
    logTerminal.innerHTML = `
        <div class="terminal-placeholder">
            <p>Terminal console cleared. Streaming fresh logs...</p>
        </div>
    `;
}

// Open inspector slide-out
function openInspector(log) {
    const code = log.attributes["legacy.user_code"] || "SYS";
    const desc = log.attributes["legacy.user_code_description"] || "System audit event";
    const platformRaw = log.resource["os.type"] || "sys";
    const severity = log.severity_text || "INFO";

    let platform = "z/OS Mainframe";
    if (platformRaw === 'ibm_i') platform = "AS/400 (IBM i)";
    if (platformRaw === 'nonstop') platform = "HPE NonStop (Tandem)";

    inspCode.textContent = code;
    inspDesc.textContent = desc;
    inspPlatform.textContent = platform;
    inspSeverity.textContent = severity;
    
    // Inject pretty JSON
    jsonCode.textContent = JSON.stringify(log, null, 4);

    inspectorDrawer.classList.add('open');
    drawerOverlay.classList.add('active');
}

// Close inspector slide-out
function closeInspector() {
    inspectorDrawer.classList.remove('open');
    drawerOverlay.classList.remove('active');
}

// Fetch stats periodically
function startStatsPolling() {
    const poll = () => {
        fetch('/api/stats')
            .then(res => res.json())
            .then(data => updateStatsUI(data))
            .catch(err => console.error("Error polling statistics API:", err));
    };

    poll(); // Initial run
    statsInterval = setInterval(poll, 1500);
}

// Update stats panels
let prevZosCount = 0;
let prevAs400Count = 0;
let prevEmsCount = 0;

function updateStatsUI(data) {
    // 1. Core Counts
    totalLogsEl.textContent = Number(data.total_processed).toLocaleString();
    zosLogsEl.textContent = Number(data.zos_count).toLocaleString();
    as400LogsEl.textContent = Number(data.as400_count).toLocaleString();
    emsLogsEl.textContent = Number(data.ems_count).toLocaleString();

    // 2. Platform Ingestion Rates (Simulate eps difference over 1.5s interval)
    if (prevZosCount > 0) {
        rateZosEl.textContent = `${Math.round((data.zos_count - prevZosCount) / 1.5)} eps`;
        rateAs400El.textContent = `${Math.round((data.as400_count - prevAs400Count) / 1.5)} eps`;
        rateEmsEl.textContent = `${Math.round((data.ems_count - prevEmsCount) / 1.5)} eps`;
    }
    prevZosCount = data.zos_count;
    prevAs400Count = data.as400_count;
    prevEmsCount = data.ems_count;

    // 3. Uptime
    uptimeEl.textContent = formatUptime(data.uptime_seconds);

    // 4. Resource gauges (simulating standard low impact Go runtime overhead)
    // CPU: 0.8% - 3.5%
    const simulatedCPU = (1.1 + Math.random() * 2.2).toFixed(1);
    cpuValueEl.textContent = `${simulatedCPU}%`;
    updateCircularGauge(cpuGauge, parseFloat(simulatedCPU), 10); // Scale up to max 10% for resolution

    // RAM: 14.5 MB - 22.0 MB
    const simulatedRAM = (14.2 + (data.total_processed % 30) / 4).toFixed(1);
    ramValueEl.textContent = `${simulatedRAM} MB`;
    updateCircularGauge(ramGauge, parseFloat(simulatedRAM), 50); // Scale up to max 50MB for resolution

    // 5. Taxonomy categories list
    renderTaxonomyBreakdown(data.code_dist, data.total_processed);
}

// Update SVG Circle gauges
function updateCircularGauge(element, value, maxVal) {
    const radius = element.r.baseVal.value;
    const circumference = 2 * Math.PI * radius;
    
    // Clamp pct
    const percentage = Math.min(value / maxVal, 1.0);
    const offset = circumference - (percentage * circumference);
    
    element.style.strokeDasharray = `${circumference}`;
    element.style.strokeDashoffset = offset;
}

// Render dynamic security event list
const codeMapNames = {
    "LL01": "Successful login",
    "LL02": "Successful logoff",
    "LL03": "User login failure",
    "SA01": "User creation",
    "SA07": "User password reset",
    "SA08": "User account locked",
    "PA01": "Successful privilege access",
    "PA02": "Failed privilege access",
    "CC01": "App configuration change",
    "CC02": "Security rules change",
    "SS01": "Application started",
    "SS02": "Application stopped",
    "SS03": "Database dump success",
    "CM01": "TMF sequencing failure",
    "CM02": "Utilization limit reached",
    "CM04": "Memory steps change"
};

function renderTaxonomyBreakdown(codeDist, totalLogs) {
    if (!codeDist) return;

    // Convert map to sorted array
    const sortedCodes = Object.entries(codeDist)
        .map(([code, count]) => ({ code, count }))
        .sort((a, b) => b.count - a.count)
        .slice(0, 5); // top 5

    taxonomyBreakdown.innerHTML = '';
    
    sortedCodes.forEach(item => {
        if (item.code === 'UNKNOWN') return;
        const name = codeMapNames[item.code] || "System Audit Record";
        const percentage = totalLogs > 0 ? (item.count / totalLogs * 100).toFixed(1) : 0;
        
        const row = document.createElement('div');
        row.className = 'taxonomy-item';
        row.innerHTML = `
            <span class="code">${item.code}</span>
            <span class="name" title="${name}">${name}</span>
            <div class="bar-track"><div class="bar-fill" style="width: ${percentage}%"></div></div>
            <span class="count">${item.count}</span>
        `;
        taxonomyBreakdown.appendChild(row);
    });
}

// Convert seconds into HH:MM:SS
function formatUptime(seconds) {
    const hrs = Math.floor(seconds / 3600);
    const mins = Math.floor((seconds % 3600) / 60);
    const secs = Math.floor(seconds % 60);
    
    return [
        hrs.toString().padStart(2, '0'),
        mins.toString().padStart(2, '0'),
        secs.toString().padStart(2, '0')
    ].join(':');
}
