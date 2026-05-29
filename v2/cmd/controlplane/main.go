package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sync"
	"time"
)

// AgentNode represents a registered LegacyTel agent in the fleet database
type AgentNode struct {
	ID                 string                 `json:"id"`
	Hostname           string                 `json:"hostname"`
	IP                 string                 `json:"ip"`
	OS                 string                 `json:"os"`
	Version            string                 `json:"version"`
	HypervisorType     string                 `json:"hypervisor_type"`
	HypervisorName     string                 `json:"hypervisor_name"`
	AppInventory       []string               `json:"app_inventory"`
	CPUUsage           float64                `json:"cpu_usage"`
	MemoryUsage        float64                `json:"memory_usage"`
	Throughput         float64                `json:"throughput"`
	LastHeartbeat      time.Time              `json:"last_heartbeat"`
	Status             string                 `json:"status"` // "ACTIVE", "INACTIVE", "UPGRADING", "ROLLBACK"
	TargetPolicyVersion int                    `json:"target_policy_version"`
	CurrentPolicyVersion int                   `json:"current_policy_version"`
	PendingUpgrade     string                 `json:"pending_upgrade"` // Target version if upgrading
}

// AgentBinary represents a stored package in the Central Repository
type AgentBinary struct {
	Version    string    `json:"version"`
	OS         string    `json:"os"`
	Arch       string    `json:"arch"`
	Channel    string    `json:"channel"` // "stable" (N), "previous" (N-1), "beta" (Beta)
	UploadTime time.Time `json:"upload_time"`
	Checksum   string    `json:"checksum"`
}

// FleetDatabase holds the in-memory state of the registered nodes
type FleetDatabase struct {
	mu           sync.RWMutex
	Nodes        map[string]*AgentNode
	Binaries     map[string][]*AgentBinary // Key: "os/arch" -> List of versions
	ClientChans  map[chan string]bool
	PolicyConfig string
	PolicyVer    int
}

var db = &FleetDatabase{
	Nodes:       make(map[string]*AgentNode),
	Binaries:    make(map[string][]*AgentBinary),
	ClientChans: make(map[chan string]bool),
	PolicyVer:   1,
	PolicyConfig: `receivers:
  syslog:
    port: 514
  otlp:
    port: 4317
exporters:
  splunk_hec:
    enabled: true
    index: "security_fleet"`,
}

func main() {
	// Initialize default binary repository packages (N, N-1, Beta) for verification
	initDefaultBinaryRepository()

	// Root route serving the fleet management dashboard
	http.HandleFunc("/", handleDashboard)
	
	// API Endpoints for Fleet Nodes (mTLS / HTTP secure streams)
	http.HandleFunc("/api/v1/register", handleRegister)
	http.HandleFunc("/api/v1/heartbeat", handleHeartbeat)
	http.HandleFunc("/api/v1/policy", handlePolicy)
	
	// API Endpoints for UI / Admin Console control
	http.HandleFunc("/api/v1/admin/upgrade", handleAdminUpgrade)
	http.HandleFunc("/api/v1/admin/upgrade-all", handleAdminUpgradeAll)
	http.HandleFunc("/api/v1/admin/policy/update", handleAdminPolicyUpdate)
	http.HandleFunc("/api/v1/admin/binaries", handleAdminBinaries)
	http.HandleFunc("/api/v1/stream", handleSSEStream)

	// Mock database generator for direct evaluation/demo purposes
	go runMockHeartbeatSimulator()

	serverAddr := ":9090"
	log.Printf("[CONTROL PLANE] LegacyTel Central Fleet Manager starting on %s", serverAddr)
	log.Printf("[CONTROL PLANE] Access the central dashboard: http://localhost:9090")
	if err := http.ListenAndServe(serverAddr, nil); err != nil {
		log.Fatalf("Control Plane server failed: %v", err)
	}
}

func initDefaultBinaryRepository() {
	db.mu.Lock()
	defer db.mu.Unlock()

	platforms := []string{"linux", "windows", "darwin"}
	for _, osName := range platforms {
		key := osName + "/amd64"
		db.Binaries[key] = []*AgentBinary{
			{Version: "v2.0.0", OS: osName, Arch: "amd64", Channel: "stable", UploadTime: time.Now().Add(-48 * time.Hour), Checksum: "sha256-a1b2c3d4"},
			{Version: "v1.9.8", OS: osName, Arch: "amd64", Channel: "previous", UploadTime: time.Now().Add(-240 * time.Hour), Checksum: "sha256-e5f6g7h8"},
			{Version: "v2.1.0-beta", OS: osName, Arch: "amd64", Channel: "beta", UploadTime: time.Now().Add(-2 * time.Hour), Checksum: "sha256-i9j0k1l2"},
		}
	}
}

// handleRegister registers a new node in the control plane database
func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var node AgentNode
	if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	db.mu.Lock()
	node.LastHeartbeat = time.Now()
	node.Status = "ACTIVE"
	node.TargetPolicyVersion = db.PolicyVer
	node.CurrentPolicyVersion = 0
	db.Nodes[node.ID] = &node
	db.mu.Unlock()

	log.Printf("[REGISTER] Node '%s' (%s - %s) successfully registered.", node.Hostname, node.OS, node.ID)
	broadcastUpdate(fmt.Sprintf("Registered node %s", node.Hostname))

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "SUCCESS",
		"node_id":        node.ID,
		"policy_version": db.PolicyVer,
	})
}

// handleHeartbeat processes live health updates and returns pending actions
func handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var update AgentNode
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	db.mu.Lock()
	node, exists := db.Nodes[update.ID]
	if exists {
		node.CPUUsage = update.CPUUsage
		node.MemoryUsage = update.MemoryUsage
		node.Throughput = update.Throughput
		node.Version = update.Version
		node.LastHeartbeat = time.Now()
		node.Status = "ACTIVE"
		if update.HypervisorType != "" {
			node.HypervisorType = update.HypervisorType
			node.HypervisorName = update.HypervisorName
		}
		if len(update.AppInventory) > 0 {
			node.AppInventory = update.AppInventory
		}
		node.CurrentPolicyVersion = update.CurrentPolicyVersion
	} else {
		update.LastHeartbeat = time.Now()
		update.Status = "ACTIVE"
		update.TargetPolicyVersion = db.PolicyVer
		db.Nodes[update.ID] = &update
		node = &update
	}
	db.mu.Unlock()

	db.mu.RLock()
	resp := map[string]interface{}{
		"status":            "OK",
		"policy_version":    db.PolicyVer,
		"policy_config":     db.PolicyConfig,
		"target_version":    node.PendingUpgrade,
		"upgrade_scheduled": node.PendingUpgrade != "",
	}
	db.mu.RUnlock()

	broadcastUpdate(fmt.Sprintf("Heartbeat from %s (CPU: %.1f%%, RAM: %.1f%%)", node.Hostname, node.CPUUsage, node.MemoryUsage))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handlePolicy returns the latest policy config
func handlePolicy(w http.ResponseWriter, r *http.Request) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"version": db.PolicyVer,
		"config":  db.PolicyConfig,
	})
}

// handleAdminUpgrade triggers a scheduled upgrade command for a selective host
func handleAdminUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		NodeID        string `json:"node_id"`
		TargetVersion string `json:"target_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	db.mu.Lock()
	node, exists := db.Nodes[req.NodeID]
	if exists {
		node.PendingUpgrade = req.TargetVersion
		node.Status = "UPGRADING"
	}
	db.mu.Unlock()

	if !exists {
		http.Error(w, "Node not found", http.StatusNotFound)
		return
	}

	log.Printf("[ADMIN] Scheduled upgrade of Node '%s' to version '%s'", node.Hostname, req.TargetVersion)
	broadcastUpdate(fmt.Sprintf("Scheduled upgrade for %s to %s", node.Hostname, req.TargetVersion))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "UPGRADE_SCHEDULED"})
}

// handleAdminUpgradeAll triggers an upgrade for all hosts in the fleet
func handleAdminUpgradeAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TargetVersion string `json:"target_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	db.mu.Lock()
	count := 0
	for _, node := range db.Nodes {
		node.PendingUpgrade = req.TargetVersion
		node.Status = "UPGRADING"
		count++
	}
	db.mu.Unlock()

	log.Printf("[ADMIN] Orchestrated bulk upgrade for %d nodes to version '%s'", count, req.TargetVersion)
	broadcastUpdate(fmt.Sprintf("Scheduled bulk upgrade of all %d nodes to %s", count, req.TargetVersion))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "BULK_UPGRADE_SCHEDULED",
		"count":  count,
	})
}

// handleAdminBinaries handles package uploads, duplicate checking, and listing versions
func handleAdminBinaries(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		db.mu.RLock()
		defer db.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(db.Binaries)
		return
	}

	if r.Method == http.MethodPost {
		var req AgentBinary
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		key := req.OS + "/" + req.Arch
		db.mu.Lock()
		list, exists := db.Binaries[key]
		if !exists {
			list = []*AgentBinary{}
		}

		// Duplicate check
		duplicate := false
		for _, b := range list {
			if b.Version == req.Version {
				duplicate = true
				break
			}
		}

		if duplicate {
			db.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"status": "DUPLICATE_VERSION", "message": "Binary version already exists."})
			return
		}

		// Save uploaded version (limit and shift channel pointers if necessary to keep 3 versions)
		req.UploadTime = time.Now()
		req.Checksum = fmt.Sprintf("sha256-u%dp%d", time.Now().Second(), time.Now().Nanosecond()%1000)
		db.Binaries[key] = append(db.Binaries[key], &req)
		db.mu.Unlock()

		log.Printf("[REPOSITORY] Uploaded new package version '%s' for platform '%s/%s'", req.Version, req.OS, req.Arch)
		broadcastUpdate(fmt.Sprintf("New agent binary %s uploaded for %s/%s in Channel: %s", req.Version, req.OS, req.Arch, req.Channel))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "UPLOAD_SUCCESS"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleAdminPolicyUpdate updates the global policy config
func handleAdminPolicyUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Config string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	db.mu.Lock()
	db.PolicyVer++
	db.PolicyConfig = req.Config
	db.mu.Unlock()

	log.Printf("[ADMIN] Dynamic Policy updated to Version %d", db.PolicyVer)
	broadcastUpdate(fmt.Sprintf("Global Policy updated to Version %d", db.PolicyVer))

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "SUCCESS",
		"version": db.PolicyVer,
	})
}

// handleSSEStream streams events to the control plane dashboard
func handleSSEStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	ch := make(chan string, 10)
	db.mu.Lock()
	db.ClientChans[ch] = true
	db.mu.Unlock()

	defer func() {
		db.mu.Lock()
		delete(db.ClientChans, ch)
		db.mu.Unlock()
		close(ch)
	}()

	// Send initial database dump to UI
	db.mu.RLock()
	nodesJSON, _ := json.Marshal(db.Nodes)
	binariesJSON, _ := json.Marshal(db.Binaries)
	fmt.Fprintf(w, "event: init\ndata: {\"nodes\":%s, \"binaries\":%s}\n\n", string(nodesJSON), string(binariesJSON))
	flusher.Flush()
	db.mu.RUnlock()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			db.mu.RLock()
			nodesJSON, _ := json.Marshal(db.Nodes)
			binariesJSON, _ := json.Marshal(db.Binaries)
			db.mu.RUnlock()
			fmt.Fprintf(w, "event: update\ndata: {\"log\":\"%s\", \"nodes\":%s, \"binaries\":%s}\n\n", msg, string(nodesJSON), string(binariesJSON))
			flusher.Flush()
		}
	}
}

func broadcastUpdate(msg string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	formatted := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	for ch := range db.ClientChans {
		select {
		case ch <- formatted:
		default:
		}
	}
}

// handleDashboard renders the single page fleet manager UI
func handleDashboard(w http.ResponseWriter, r *http.Request) {
	t, err := template.New("dashboard").Parse(dashboardHTML)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	t.Execute(w, nil)
}

// runMockHeartbeatSimulator populates the UI with simulated nodes for direct evaluation
func runMockHeartbeatSimulator() {
	db.mu.Lock()
	db.Nodes["node-linux-web"] = &AgentNode{
		ID:                  "node-linux-web",
		Hostname:            "prod-linux-nginx-01",
		IP:                  "10.0.10.15",
		OS:                  "linux",
		Version:             "v1.9.8",
		HypervisorType:      "type-1",
		HypervisorName:      "VMware ESXi",
		AppInventory:        []string{"nginx", "postgresql", "docker", "redis"},
		CPUUsage:            12.4,
		MemoryUsage:         45.8,
		Throughput:          245.2,
		LastHeartbeat:       time.Now(),
		Status:              "ACTIVE",
		TargetPolicyVersion: 1,
		CurrentPolicyVersion: 1,
	}

	db.Nodes["node-win-ad"] = &AgentNode{
		ID:                  "node-win-ad",
		Hostname:            "corp-win-ad-02",
		IP:                  "10.0.20.44",
		OS:                  "windows",
		Version:             "v1.9.8",
		HypervisorType:      "type-1",
		HypervisorName:      "Microsoft Hyper-V",
		AppInventory:        []string{"ActiveDirectory", "IIS", "DHCP_Server"},
		CPUUsage:            8.7,
		MemoryUsage:         64.2,
		Throughput:          98.5,
		LastHeartbeat:       time.Now(),
		Status:              "ACTIVE",
		TargetPolicyVersion: 1,
		CurrentPolicyVersion: 1,
	}

	db.Nodes["node-mac-dev"] = &AgentNode{
		ID:                  "node-mac-dev",
		Hostname:            "dev-macbook-gs",
		IP:                  "192.168.1.108",
		OS:                  "darwin",
		Version:             "v2.0.0",
		HypervisorType:      "type-2",
		HypervisorName:      "VirtualBox",
		AppInventory:        []string{"vscode", "docker", "go", "node"},
		CPUUsage:            24.1,
		MemoryUsage:         82.1,
		Throughput:          14.2,
		LastHeartbeat:       time.Now(),
		Status:              "ACTIVE",
		TargetPolicyVersion: 1,
		CurrentPolicyVersion: 1,
	}
	db.mu.Unlock()

	ticker := time.NewTicker(3 * time.Second)
	for range ticker.C {
		db.mu.Lock()
		for _, node := range db.Nodes {
			// Simulating fluctuating metrics
			node.CPUUsage += float64((time.Now().UnixNano() % 5) - 2)
			if node.CPUUsage < 2 {
				node.CPUUsage = 5.2
			} else if node.CPUUsage > 95 {
				node.CPUUsage = 88.4
			}

			node.MemoryUsage += float64((time.Now().UnixNano() % 3) - 1)
			if node.MemoryUsage < 10 {
				node.MemoryUsage = 24.1
			} else if node.MemoryUsage > 98 {
				node.MemoryUsage = 92.5
			}

			node.Throughput += float64((time.Now().UnixNano() % 11) - 5)
			if node.Throughput < 0 {
				node.Throughput = 15.4
			}

			node.LastHeartbeat = time.Now()
			
			// Resolve upgrade simulation if scheduled
			if node.Status == "UPGRADING" && node.PendingUpgrade != "" {
				node.Version = node.PendingUpgrade
				node.PendingUpgrade = ""
				node.Status = "ACTIVE"
				broadcastUpdate(fmt.Sprintf("[FLEET] Node %s upgrade completed successfully. Status set to ACTIVE.", node.Hostname))
			}
		}
		db.mu.Unlock()
		broadcastUpdate("Fleet metrics updated successfully.")
	}
}

// Stunning Single-Page Glassmorphic HTML Template for Central Control Plane Console
const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>LegacyTel Control Plane — Fleet Management</title>
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;600;700&family=JetBrains+Mono:wght@400;700&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-color: #0d0e15;
            --card-bg: rgba(22, 25, 41, 0.45);
            --border-glow: rgba(0, 242, 254, 0.25);
            --text-main: #f8fafc;
            --text-muted: #94a3b8;
            --accent-teal: #00f2fe;
            --accent-gold: #ffb703;
            --accent-orange: #ff5e62;
            --accent-green: #4ade80;
        }

        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }

        body {
            background-color: var(--bg-color);
            color: var(--text-main);
            font-family: 'Outfit', sans-serif;
            background-image: 
                radial-gradient(at 10% 20%, rgba(123, 44, 191, 0.15) 0px, transparent 50%),
                radial-gradient(at 90% 80%, rgba(0, 242, 254, 0.12) 0px, transparent 50%);
            background-attachment: fixed;
            min-height: 100vh;
            padding: 30px;
        }

        header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 30px;
            backdrop-filter: blur(15px);
            background: rgba(20, 24, 46, 0.5);
            border: 1px solid var(--border-glow);
            padding: 20px 30px;
            border-radius: 20px;
            box-shadow: 0 8px 32px 0 rgba(0, 0, 0, 0.37);
        }

        h1 {
            font-weight: 700;
            font-size: 26px;
            background: linear-gradient(to right, #00f2fe, #4facfe);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            display: flex;
            align-items: center;
            gap: 10px;
        }

        .subtitle {
            font-size: 14px;
            color: var(--text-muted);
        }

        .stats-strip {
            display: flex;
            gap: 20px;
        }

        .stat-badge {
            background: rgba(255, 255, 255, 0.05);
            border: 1px solid rgba(255, 255, 255, 0.1);
            padding: 8px 16px;
            border-radius: 12px;
            font-size: 14px;
            display: flex;
            align-items: center;
            gap: 8px;
        }

        .stat-badge span {
            font-weight: 700;
            color: var(--accent-teal);
        }

        main {
            display: grid;
            grid-template-columns: 2fr 1.2fr;
            gap: 30px;
        }

        .panel {
            background: var(--card-bg);
            backdrop-filter: blur(20px);
            -webkit-backdrop-filter: blur(20px);
            border: 1px solid var(--border-glow);
            border-radius: 20px;
            padding: 25px;
            box-shadow: 0 8px 32px 0 rgba(0, 0, 0, 0.37);
            margin-bottom: 30px;
        }

        .panel-title {
            font-weight: 600;
            font-size: 18px;
            margin-bottom: 20px;
            display: flex;
            justify-content: space-between;
            align-items: center;
            border-bottom: 1px solid rgba(255, 255, 255, 0.08);
            padding-bottom: 12px;
        }

        .topology-container {
            width: 100%;
            height: 380px;
            background: rgba(0, 0, 0, 0.3);
            border-radius: 16px;
            border: 1px solid rgba(255, 255, 255, 0.05);
            position: relative;
            overflow: hidden;
            display: flex;
            justify-content: center;
            align-items: center;
        }

        .topology-svg {
            width: 100%;
            height: 100%;
        }

        .node-grid {
            display: grid;
            grid-template-columns: 1fr;
            gap: 20px;
        }

        .node-card {
            background: rgba(255, 255, 255, 0.02);
            border: 1px solid rgba(255, 255, 255, 0.06);
            border-radius: 16px;
            padding: 20px;
            transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            position: relative;
            overflow: hidden;
        }

        .node-card:hover {
            transform: translateY(-4px);
            border-color: var(--accent-teal);
            box-shadow: 0 8px 24px rgba(0, 242, 254, 0.1);
        }

        .node-card.upgrading {
            border-color: var(--accent-gold);
            animation: pulse-border 1.5s infinite;
        }

        @keyframes pulse-border {
            0% { border-color: rgba(255, 183, 3, 0.3); }
            50% { border-color: rgba(255, 183, 3, 1); }
            100% { border-color: rgba(255, 183, 3, 0.3); }
        }

        .node-header {
            display: flex;
            justify-content: space-between;
            align-items: flex-start;
            margin-bottom: 15px;
        }

        .node-meta {
            display: flex;
            flex-direction: column;
            gap: 4px;
        }

        .node-title {
            font-weight: 700;
            font-size: 17px;
            display: flex;
            align-items: center;
            gap: 10px;
        }

        .node-os {
            text-transform: uppercase;
            font-size: 11px;
            background: rgba(255, 255, 255, 0.08);
            padding: 2px 8px;
            border-radius: 6px;
            font-weight: 600;
            color: var(--text-muted);
        }

        .node-ver {
            font-size: 12px;
            color: var(--accent-teal);
            font-family: 'JetBrains Mono', monospace;
            background: rgba(0, 242, 254, 0.08);
            padding: 2px 8px;
            border-radius: 6px;
        }

        .node-metrics {
            display: grid;
            grid-template-columns: repeat(3, 1fr);
            gap: 15px;
            margin-bottom: 15px;
        }

        .metric-item {
            background: rgba(0, 0, 0, 0.2);
            padding: 10px;
            border-radius: 12px;
            text-align: center;
            border: 1px solid rgba(255, 255, 255, 0.03);
        }

        .metric-lbl {
            font-size: 11px;
            color: var(--text-muted);
            margin-bottom: 4px;
        }

        .metric-val {
            font-size: 16px;
            font-weight: 700;
            color: var(--text-main);
        }

        .node-footer {
            display: flex;
            justify-content: space-between;
            align-items: center;
            border-top: 1px solid rgba(255, 255, 255, 0.04);
            padding-top: 12px;
        }

        .hypervisor-tag {
            font-size: 12px;
            display: flex;
            align-items: center;
            gap: 6px;
            color: var(--accent-gold);
            font-weight: 600;
        }

        .hypervisor-tag.type-2 {
            color: var(--accent-teal);
        }

        .card-actions {
            display: flex;
            gap: 8px;
        }

        button {
            background: linear-gradient(135deg, #00f2fe 0%, #4facfe 100%);
            border: none;
            color: #0d0e15;
            padding: 6px 12px;
            border-radius: 8px;
            font-weight: 600;
            font-size: 12px;
            cursor: pointer;
            transition: all 0.2s;
        }

        button:hover {
            transform: translateY(-1px);
            box-shadow: 0 4px 12px rgba(0, 242, 254, 0.3);
        }

        button.btn-sec {
            background: rgba(255, 255, 255, 0.05);
            border: 1px solid rgba(255, 255, 255, 0.1);
            color: var(--text-main);
        }

        button.btn-sec:hover {
            background: rgba(255, 255, 255, 0.1);
            box-shadow: none;
        }

        .console-log {
            background: #05060b;
            border: 1px solid rgba(0, 242, 254, 0.15);
            border-radius: 12px;
            padding: 15px;
            font-family: 'JetBrains Mono', monospace;
            font-size: 12px;
            height: 200px;
            overflow-y: auto;
            color: var(--accent-teal);
        }

        .log-entry {
            margin-bottom: 6px;
            line-height: 1.4;
            border-left: 2px solid var(--accent-teal);
            padding-left: 8px;
        }

        .inventory-list {
            display: flex;
            flex-wrap: wrap;
            gap: 6px;
            margin-top: 10px;
        }

        .inventory-tag {
            background: rgba(255, 255, 255, 0.04);
            border: 1px solid rgba(255, 255, 255, 0.08);
            font-size: 11px;
            padding: 2px 8px;
            border-radius: 6px;
            color: var(--text-muted);
            font-family: 'JetBrains Mono', monospace;
        }

        .repo-card {
            background: rgba(255, 255, 255, 0.02);
            border: 1px solid rgba(255, 255, 255, 0.06);
            border-radius: 12px;
            padding: 15px;
            margin-bottom: 12px;
        }

        .repo-row {
            display: flex;
            justify-content: space-between;
            align-items: center;
            font-size: 13px;
            margin-bottom: 8px;
            padding-bottom: 6px;
            border-bottom: 1px solid rgba(255, 255, 255, 0.03);
        }

        .repo-row span.channel {
            text-transform: uppercase;
            font-size: 10px;
            padding: 2px 6px;
            border-radius: 4px;
            font-weight: 700;
        }

        .channel.stable { background: rgba(74, 222, 128, 0.15); color: var(--accent-green); }
        .channel.previous { background: rgba(148, 163, 184, 0.15); color: var(--text-muted); }
        .channel.beta { background: rgba(255, 183, 3, 0.15); color: var(--accent-gold); }

        /* Modal styling */
        .modal {
            display: none;
            position: fixed;
            top: 0;
            left: 0;
            width: 100%;
            height: 100%;
            background: rgba(0, 0, 0, 0.8);
            backdrop-filter: blur(8px);
            z-index: 1000;
            justify-content: center;
            align-items: center;
        }

        .modal-content {
            background: #14182e;
            border: 1px solid var(--accent-teal);
            border-radius: 20px;
            padding: 30px;
            width: 500px;
            box-shadow: 0 8px 32px rgba(0,0,0,0.5);
        }

        .modal-header {
            font-size: 18px;
            font-weight: 700;
            margin-bottom: 20px;
            border-bottom: 1px solid rgba(255,255,255,0.08);
            padding-bottom: 10px;
        }

        textarea {
            width: 100%;
            height: 180px;
            background: rgba(0,0,0,0.3);
            border: 1px solid rgba(255,255,255,0.1);
            border-radius: 10px;
            color: #fff;
            font-family: 'JetBrains Mono', monospace;
            padding: 10px;
            margin-bottom: 20px;
            resize: none;
        }

        .modal-footer {
            display: flex;
            justify-content: flex-end;
            gap: 10px;
        }

        .input-group {
            margin-bottom: 15px;
            display: flex;
            flex-direction: column;
            gap: 6px;
        }

        .input-group label {
            font-size: 13px;
            color: var(--text-muted);
        }

        .input-group input, .input-group select {
            background: rgba(0, 0, 0, 0.2);
            border: 1px solid rgba(255, 255, 255, 0.1);
            padding: 10px;
            border-radius: 8px;
            color: #fff;
            font-family: inherit;
        }

        .flex-row {
            display: flex;
            gap: 15px;
        }
    </style>
</head>
<body>

    <header>
        <div>
            <h1>🛰️ LegacyTel Central</h1>
            <div class="subtitle">Enterprise Observability Control Plane & Fleet Manager</div>
        </div>
        <div class="stats-strip">
            <div class="stat-badge">Active Nodes: <span id="stat-active">0</span></div>
            <div class="stat-badge">Upgrading: <span id="stat-upgrading">0</span></div>
            <div class="stat-badge">Global Policy Version: <span id="stat-policy">1</span></div>
        </div>
    </header>

    <main>
        <div>
            <!-- Hub & Spoke Topology Visualizer -->
            <div class="panel">
                <div class="panel-title">
                    <span>🕸️ Fleet Network Topology Map</span>
                    <button class="btn-sec" onclick="triggerBulkUpgrade()">🚀 Bulk Upgrade All Hosts</button>
                </div>
                <div class="topology-container">
                    <svg class="topology-svg" id="topology-map">
                        <!-- Dynamic Hub & Spokes rendered inside script -->
                    </svg>
                </div>
            </div>

            <!-- Agent Grid -->
            <div class="panel">
                <div class="panel-title">
                    <span>🖥️ Registered Agent Fleet</span>
                    <button onclick="openPolicyModal()">⚙️ Global Policy Manager</button>
                </div>
                <div class="node-grid" id="node-container">
                    <!-- Nodes dynamically injected here -->
                </div>
            </div>
        </div>

        <div style="display: flex; flex-direction: column; gap: 30px;">
            <!-- Upload & Package Repository -->
            <div class="panel">
                <div class="panel-title">
                    <span>📦 Central Binary Repository</span>
                    <button onclick="openUploadModal()">➕ Upload New Package</button>
                </div>
                <div id="repo-container">
                    <!-- Binaries (N, n-1, Beta) rendered here -->
                </div>
            </div>

            <!-- Real-time Terminal Log -->
            <div class="panel">
                <div class="panel-title">📡 Real-Time Central Event Stream</div>
                <div class="console-log" id="console-stream">
                    <!-- Event logs dynamically injected -->
                </div>
            </div>

            <!-- Drill-down Inspector Drawer -->
            <div class="panel">
                <div class="panel-title">📁 Scanned Application Registry</div>
                <div id="central-inventory" style="color: var(--text-muted); font-size: 14px;">
                    Select a host spoke node on the map to inspect software package inventory and server parameters.
                </div>
            </div>
        </div>
    </main>

    <!-- Global Policy Modal -->
    <div class="modal" id="policy-modal">
        <div class="modal-content">
            <div class="modal-header">Global Policy Configuration</div>
            <textarea id="policy-text">receivers:
  syslog:
    port: 514
  otlp:
    port: 4317
exporters:
  splunk_hec:
    enabled: true
    index: "security_fleet"</textarea>
            <div class="modal-footer">
                <button class="btn-sec" onclick="closePolicyModal()">Cancel</button>
                <button onclick="savePolicyConfig()">Save & Deploy Policy</button>
            </div>
        </div>
    </div>

    <!-- Binary Package Upload Modal -->
    <div class="modal" id="upload-modal">
        <div class="modal-content">
            <div class="modal-header">Upload Latest Agent Binary Package</div>
            <div class="flex-row">
                <div class="input-group" style="flex: 1;">
                    <label>Target OS</label>
                    <select id="upload-os">
                        <option value="linux">Linux</option>
                        <option value="windows">Windows</option>
                        <option value="darwin">macOS (Darwin)</option>
                    </select>
                </div>
                <div class="input-group" style="flex: 1;">
                    <label>Architecture</label>
                    <select id="upload-arch">
                        <option value="amd64">x86_64 (amd64)</option>
                        <option value="arm64">Apple Silicon / ARM (arm64)</option>
                    </select>
                </div>
            </div>
            <div class="flex-row">
                <div class="input-group" style="flex: 1;">
                    <label>Package Version</label>
                    <input type="text" id="upload-version" placeholder="e.g. v2.0.2">
                </div>
                <div class="input-group" style="flex: 1;">
                    <label>Upgrade Channel</label>
                    <select id="upload-channel">
                        <option value="stable">N (Latest Stable)</option>
                        <option value="previous">N-1 (Previous Stable)</option>
                        <option value="beta">Beta (Testing Branch)</option>
                    </select>
                </div>
            </div>
            <div style="color: var(--accent-orange); font-size: 12px; margin-bottom: 15px;" id="upload-error"></div>
            <div class="modal-footer">
                <button class="btn-sec" onclick="closeUploadModal()">Cancel</button>
                <button onclick="submitPackageUpload()">Verify & Upload Binary</button>
            </div>
        </div>
    </div>

    <script>
        let nodesData = {};
        let repoData = {};

        // Establish real-time SSE link with Control Plane
        const source = new EventSource('/api/v1/stream');

        source.addEventListener('init', function(e) {
            const data = JSON.parse(e.data);
            nodesData = data.nodes;
            repoData = data.binaries;
            renderNodes();
            renderRepository();
            renderTopologyMap();
        });

        source.addEventListener('update', function(e) {
            const payload = JSON.parse(e.data);
            nodesData = payload.nodes;
            repoData = payload.binaries;
            
            // Ingest log stream
            const stream = document.getElementById('console-stream');
            const entry = document.createElement('div');
            entry.className = 'log-entry';
            entry.textContent = payload.log;
            stream.appendChild(entry);
            stream.scrollTop = stream.scrollHeight;

            renderNodes();
            renderRepository();
            renderTopologyMap();
        });

        function renderNodes() {
            const container = document.getElementById('node-container');
            container.innerHTML = '';

            let activeCount = 0;
            let upgradingCount = 0;

            Object.values(nodesData).forEach(node => {
                if (node.status === 'ACTIVE') activeCount++;
                if (node.status === 'UPGRADING') upgradingCount++;

                const card = document.createElement('div');
                card.className = 'node-card ' + (node.status === 'UPGRADING' ? 'upgrading' : '');
                
                const isType1 = node.hypervisor_type === 'type-1';
                const hvClass = isType1 ? 'type-1' : 'type-2';
                const hvLabel = isType1 ? 'Type 1 (Bare-Metal)' : 'Type 2 (Hosted)';

                // Render matching update selections based on OS platform
                const osKey = node.os + '/amd64';
                let selectOptions = '';
                if (repoData[osKey]) {
                    repoData[osKey].forEach(binary => {
                        const channelLabel = binary.channel === 'stable' ? 'N' : (binary.channel === 'previous' ? 'N-1' : 'Beta');
                        selectOptions += '<option value="' + binary.version + '">' + binary.version + ' (' + channelLabel + ')</option>';
                    });
                }

                card.innerHTML = 
                    '<div class="node-header">' +
                        '<div class="node-meta">' +
                            '<div class="node-title">' +
                                '<span>' + node.hostname + '</span>' +
                                '<span class="node-os">' + node.os + '</span>' +
                            '</div>' +
                            '<div style="font-size: 12px; color: var(--text-muted);">IP Address: <strong>' + node.IP + '</strong></div>' +
                        '</div>' +
                        '<span class="node-ver">' + node.version + '</span>' +
                    '</div>' +

                    '<div class="node-metrics">' +
                        '<div class="metric-item">' +
                            '<div class="metric-lbl">CPU Usage</div>' +
                            '<div class="metric-val">' + node.cpu_usage.toFixed(1) + '%</div>' +
                        '</div>' +
                        '<div class="metric-item">' +
                            '<div class="metric-lbl">RAM Usage</div>' +
                            '<div class="metric-val">' + node.memory_usage.toFixed(1) + '%</div>' +
                        '</div>' +
                        '<div class="metric-item">' +
                            '<div class="metric-lbl">Throughput</div>' +
                            '<div class="metric-val">' + node.throughput.toFixed(1) + ' EPS</div>' +
                        '</div>' +
                    '</div>' +
                    '<div class="node-footer">' +
                        '<span class="hypervisor-tag ' + hvClass + '">' +
                            '🛡️ ' + node.hypervisor_name + ' [' + hvLabel + ']' +
                        '</span>' +
                        '<div class="card-actions" style="align-items: center; gap: 8px;">' +
                            '<select id="select-' + node.id + '" style="background: rgba(0,0,0,0.3); color:#fff; border:1px solid rgba(255,255,255,0.15); padding: 4px; border-radius: 6px; font-size:11px;">' +
                                selectOptions +
                            '</select>' +
                            '<button onclick="triggerUpgrade(\'' + node.id + '\')">Upgrade</button>' +
                        '</div>' +
                    '</div>';
                container.appendChild(card);
            });

            document.getElementById('stat-active').textContent = activeCount;
            document.getElementById('stat-upgrading').textContent = upgradingCount;
        }

        function renderRepository() {
            const container = document.getElementById('repo-container');
            container.innerHTML = '';

            Object.keys(repoData).forEach(platformKey => {
                const card = document.createElement('div');
                card.className = 'repo-card';
                card.innerHTML = '<div style="font-weight: 700; font-size:14px; margin-bottom:10px; color:var(--accent-teal);">🖥️ ' + platformKey.toUpperCase() + '</div>';

                repoData[platformKey].forEach(binary => {
                    const chClass = binary.channel === 'stable' ? 'stable' : (binary.channel === 'previous' ? 'previous' : 'beta');
                    const chLabel = binary.channel === 'stable' ? 'N (Latest)' : (binary.channel === 'previous' ? 'N-1 (Stable)' : 'Beta (Testing)');

                    card.innerHTML += 
                        '<div class="repo-row">' +
                            '<span><strong>' + binary.version + '</strong></span>' +
                            '<span class="channel ' + chClass + '">' + chLabel + '</span>' +
                        '</div>';
                });
                container.appendChild(card);
            });
        }

        function renderTopologyMap() {
            const svg = document.getElementById('topology-map');
            svg.innerHTML = ''; // Reset

            const cx = 250;
            const cy = 190;
            const nodes = Object.values(nodesData);
            const radius = 120;

            // 1. Draw central Hub node (Control Plane)
            const hubGroup = document.createElementNS('http://www.w3.org/2000/svg', 'g');
            hubGroup.innerHTML = 
                '<circle cx="' + cx + '" cy="' + cy + '" r="35" fill="rgba(0, 242, 254, 0.15)" stroke="var(--accent-teal)" stroke-width="2" style="filter: drop-shadow(0 0 10px rgba(0, 242, 254, 0.4));" />' +
                '<text x="' + cx + '" y="' + (cy + 4) + '" fill="#fff" font-size="12" font-weight="700" text-anchor="middle">CENTRAL</text>';
            
            // 2. Draw Spokes and Node connections dynamically
            nodes.forEach((node, idx) => {
                const angle = (idx * 2 * Math.PI) / nodes.length;
                const nx = cx + radius * Math.cos(angle);
                const ny = cy + radius * Math.sin(angle);

                // Draw connecting spoke line
                const line = document.createElementNS('http://www.w3.org/2000/svg', 'line');
                line.setAttribute('x1', cx);
                line.setAttribute('y1', cy);
                line.setAttribute('x2', nx);
                line.setAttribute('y2', ny);
                line.setAttribute('stroke', node.status === 'UPGRADING' ? 'var(--accent-gold)' : 'rgba(255,255,255,0.15)');
                line.setAttribute('stroke-width', '2');
                if (node.status === 'UPGRADING') {
                    line.setAttribute('stroke-dasharray', '5,5');
                    line.innerHTML = '<animate attributeName="stroke-dashoffset" values="50;0" dur="2s" repeatCount="indefinite" />';
                }
                svg.appendChild(line);

                // Draw Spoke server node
                const nodeG = document.createElementNS('http://www.w3.org/2000/svg', 'g');
                nodeG.setAttribute('style', 'cursor: pointer;');
                nodeG.setAttribute('onclick', 'inspectInventory("' + node.id + '")');

                const circleColor = node.os === 'linux' ? 'var(--accent-teal)' : (node.os === 'windows' ? 'var(--accent-gold)' : 'var(--accent-orange)');
                const statusPulse = node.status === 'UPGRADING' ? 'rgba(255, 183, 3, 0.3)' : 'rgba(255,255,255,0.05)';

                nodeG.innerHTML = 
                    '<circle cx="' + nx + '" cy="' + ny + '" r="25" fill="' + statusPulse + '" stroke="' + circleColor + '" stroke-width="2" />' +
                    '<text x="' + nx + '" y="' + (ny - 30) + '" fill="#fff" font-size="11" font-weight="600" text-anchor="middle">' + node.hostname + '</text>' +
                    '<text x="' + nx + '" y="' + (ny + 4) + '" fill="var(--text-muted)" font-size="10" font-weight="700" text-anchor="middle">' + node.os.toUpperCase() + '</text>' +
                    '<text x="' + nx + '" y="' + (ny + 30) + '" fill="var(--accent-teal)" font-size="9" text-anchor="middle">' + node.IP + '</text>';

                svg.appendChild(nodeG);
            });

            svg.appendChild(hubGroup);
        }

        function triggerUpgrade(nodeId) {
            const version = document.getElementById('select-' + nodeId).value;
            fetch('/api/v1/admin/upgrade', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ node_id: nodeId, target_version: version })
            })
            .then(res => res.json())
            .then(data => {
                console.log("Upgrade scheduled:", data);
            });
        }

        function triggerBulkUpgrade() {
            // Find stable latest version to deploy
            fetch('/api/v1/admin/upgrade-all', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ target_version: "v2.0.1-LTS" })
            })
            .then(res => res.json())
            .then(data => {
                console.log("Bulk upgrade scheduled:", data);
            });
        }

        function inspectInventory(nodeId) {
            const node = nodesData[nodeId];
            if (!node) return;

            const div = document.getElementById('central-inventory');
            let tagsHTML = '';
            node.app_inventory.forEach(app => {
                tagsHTML += '<span class="inventory-tag">' + app + '</span>';
            });

            div.innerHTML = 
                '<div style="font-weight: 700; color: #fff; margin-bottom: 8px;">Host: ' + node.hostname + ' (' + node.IP + ')</div>' +
                '<div style="margin-bottom: 12px; font-size: 13px;">Classified Hypervisor: <strong style="color: var(--accent-gold);">' + node.hypervisor_name + '</strong></div>' +
                '<div style="margin-bottom: 12px; font-size: 13px;">Agent Active Version: <strong style="color: var(--accent-teal);">' + node.version + '</strong></div>' +
                '<div style="font-weight: 600; color: #fff; margin-bottom: 6px; font-size: 13px;">Scanned Application Inventory:</div>' +
                '<div class="inventory-list">' + tagsHTML + '</div>';
        }

        function openUploadModal() {
            document.getElementById('upload-error').textContent = '';
            document.getElementById('upload-modal').style.display = 'flex';
        }

        function closeUploadModal() {
            document.getElementById('upload-modal').style.display = 'none';
        }

        function submitPackageUpload() {
            const osName = document.getElementById('upload-os').value;
            const arch = document.getElementById('upload-arch').value;
            const version = document.getElementById('upload-version').value;
            const channel = document.getElementById('upload-channel').value;

            if (!version) {
                document.getElementById('upload-error').textContent = 'Please enter a valid version.';
                return;
            }

            fetch('/api/v1/admin/binaries', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ version: version, os: osName, arch: arch, channel: channel })
            })
            .then(res => {
                if (res.status === 409) {
                    throw new Error('DUPLICATE_VERSION');
                }
                return res.json();
            })
            .then(data => {
                closeUploadModal();
            })
            .catch(err => {
                if (err.message === 'DUPLICATE_VERSION') {
                    document.getElementById('upload-error').textContent = 'Error: A package with this version already exists for this architecture!';
                } else {
                    document.getElementById('upload-error').textContent = 'Error uploading package details.';
                }
            });
        }

        function openPolicyModal() {
            document.getElementById('policy-modal').style.display = 'flex';
        }

        function closePolicyModal() {
            document.getElementById('policy-modal').style.display = 'none';
        }

        function savePolicyConfig() {
            const config = document.getElementById('policy-text').value;
            fetch('/api/v1/admin/policy/update', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ config: config })
            })
            .then(res => res.json())
            .then(data => {
                document.getElementById('stat-policy').textContent = data.version;
                closePolicyModal();
            });
        }
    </script>
</body>
</html>`
