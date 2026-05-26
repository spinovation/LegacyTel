package dashboard

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"legacytel/pkg/config"
	"legacytel/pkg/model"
	"legacytel/pkg/processor"
)

type DashboardServer struct {
	cfg        config.ServerConfig
	stats      *processor.TelemetryStats
	clients    map[chan *model.LogRecord]bool
	clientsMu  sync.Mutex
	assetsPath string
}

func NewDashboardServer(cfg config.ServerConfig, stats *processor.TelemetryStats, assetsPath string) *DashboardServer {
	return &DashboardServer{
		cfg:        cfg,
		stats:      stats,
		clients:    make(map[chan *model.LogRecord]bool),
		assetsPath: assetsPath,
	}
}

// Broadcast sends a new LogRecord to all active SSE streams.
func (ds *DashboardServer) Broadcast(lr *model.LogRecord) {
	ds.clientsMu.Lock()
	defer ds.clientsMu.Unlock()

	for clientChan := range ds.clients {
		select {
		case clientChan <- lr:
		default:
			// Client channel full, drop message to prevent blocking the agent
		}
	}
}

// Start launches the HTTP server for the observability dashboard
func (ds *DashboardServer) Start() error {
	mux := http.NewServeMux()

	// 1. Serve Static Assets
	mux.HandleFunc("/", ds.handleStatic)

	// 2. Metrics endpoint
	mux.HandleFunc("/api/stats", ds.handleStats)

	// 3. SSE Log Stream endpoint
	mux.HandleFunc("/api/stream", ds.handleStream)

	addr := fmt.Sprintf("%s:%d", ds.cfg.Host, ds.cfg.Port)
	log.Printf("[INFO] Observability Dashboard listening on http://%s\n", addr)
	
	// Start HTTP server
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("[FATAL] Dashboard server failed: %v", err)
		}
	}()

	return nil
}

func (ds *DashboardServer) handleStatic(w http.ResponseWriter, r *http.Request) {
	// Serve index.html, style.css, app.js
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	fullPath := filepath.Join(ds.assetsPath, path)
	
	// Security check to prevent directory traversal
	cleanedFullPath := filepath.Clean(fullPath)
	if _, err := os.Stat(cleanedFullPath); os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, cleanedFullPath)
}

func (ds *DashboardServer) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	snapshot := ds.stats.GetSnapshot()
	json.NewEncoder(w).Encode(snapshot)
}

func (ds *DashboardServer) handleStream(w http.ResponseWriter, r *http.Request) {
	// Set headers for Server-Sent Events (SSE)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create client channel
	clientChan := make(chan *model.LogRecord, 100)

	// Register client
	ds.clientsMu.Lock()
	ds.clients[clientChan] = true
	ds.clientsMu.Unlock()

	log.Printf("[INFO] Dashboard Server: New live stream client connected. Active clients: %d\n", len(ds.clients))

	// Remove client on disconnect
	defer func() {
		ds.clientsMu.Lock()
		delete(ds.clients, clientChan)
		ds.clientsMu.Unlock()
		close(clientChan)
		log.Println("[INFO] Dashboard Server: Live stream client disconnected.")
	}()

	// Event loop sending events to browser
	for {
		select {
		case <-r.Context().Done():
			return
		case lr := <-clientChan:
			data, err := json.Marshal(lr)
			if err != nil {
				continue
			}
			// Write in standard Server-Sent Event format: "data: <json>\n\n"
			fmt.Fprintf(w, "data: %s\n\n", data)
			
			// Flush the buffer to send immediately
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}
