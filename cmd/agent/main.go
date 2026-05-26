package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"legacytel/pkg/config"
	"legacytel/pkg/dashboard"
	"legacytel/pkg/exporter"
	"legacytel/pkg/model"
	"legacytel/pkg/processor"
	"legacytel/pkg/receiver"
)

func main() {
	// Parse CLI flags
	configPath := flag.String("config", "config.yaml", "Path to LegacyTel config.yaml")
	assetsPath := flag.String("assets", "", "Path to dashboard assets directory (default searches local directories)")
	flag.Parse()

	log.Println("[INFO] Starting LegacyTel Observability Agent...")

	// 1. Load Configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("[WARN] Config load failed: %v. Using production defaults.", err)
		// Fallback configuration if file not found
		cfg = &config.Config{
			Server: config.ServerConfig{Host: "0.0.0.0", Port: 8080, EnableDashboard: true},
		}
	}

	// Resolve assets path if not explicitly provided
	finalAssetsPath := *assetsPath
	if finalAssetsPath == "" {
		// Check standard layout directories relative to binary execution path
		pathsToTry := []string{
			"pkg/dashboard/assets",
			"../pkg/dashboard/assets",
			"./assets",
		}
		for _, p := range pathsToTry {
			if _, err := os.Stat(p); err == nil {
				finalAssetsPath = p
				break
			}
		}
	}
	
	if finalAssetsPath == "" {
		log.Println("[WARN] Dashboard assets directory not found. Visual UI will be inactive.")
	} else {
		log.Printf("[INFO] Serving dashboard assets from: %s\n", finalAssetsPath)
	}

	// Context for operational safety
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Initialize Normalization & Telemetry engine
	stats := processor.NewTelemetryStats()

	// 3. Initialize Outbound Exporter Pipeline
	exporterMgr := exporter.NewExporterManager(cfg.Exporters)
	exporterMgr.Start()
	defer exporterMgr.Stop()

	// 4. Initialize Dashboard Server
	var dashServer *dashboard.DashboardServer
	if cfg.Server.EnableDashboard && finalAssetsPath != "" {
		dashServer = dashboard.NewDashboardServer(cfg.Server, stats, finalAssetsPath)
		if err := dashServer.Start(); err != nil {
			log.Printf("[ERROR] Failed to start visual dashboard: %v\n", err)
		}
	}

	// 5. Initialize Legacy Platform Receivers
	var receivers []receiver.Receiver
	outputChan := make(chan *model.LogRecord, 1000)

	if cfg.Receivers.ZOS.Enabled {
		rec := receiver.NewSMFReceiver(cfg.Receivers.ZOS)
		receivers = append(receivers, rec)
	}

	if cfg.Receivers.AS400.Enabled {
		rec := receiver.NewQAUDJRNReceiver(cfg.Receivers.AS400)
		receivers = append(receivers, rec)
	}

	if cfg.Receivers.EMS.Enabled {
		rec := receiver.NewEMSReceiver(cfg.Receivers.EMS)
		receivers = append(receivers, rec)
	}

	// Start all inputs
	for _, rec := range receivers {
		log.Printf("[INFO] Starting legacy receiver: %s\n", rec.GetName())
		if err := rec.Start(ctx, outputChan); err != nil {
			log.Printf("[ERROR] Failed to start receiver %s: %v\n", rec.GetName(), err)
		}
	}

	// 6. Main Pipeline Router
	go func() {
		// Emit initial heartbeat on agent startup (immediate activity/status log)
		now := time.Now()
		outputChan <- &model.LogRecord{
			Timestamp:         now,
			ObservedTimestamp: now,
			SeverityText:      "INFO",
			SeverityNumber:    model.OTelSeverityNumber("INFO"),
			Body:              "LegacyTel Agent Heartbeat - Observability pipeline initialized and monitoring legacy endpoints.",
			Attributes: map[string]interface{}{
				"legacy.user_code": "SS05",
				"system.status":    "ACTIVE",
				"agent.version":    "1.0.0",
			},
			Resource: map[string]interface{}{
				"host.name": "localhost",
				"os.type":   "system",
			},
		}

		// Set up daily heartbeat ticker (24 hours)
		heartbeatTicker := time.NewTicker(24 * time.Hour)
		defer heartbeatTicker.Stop()

		// Background routine to dispatch standard 24-hour heartbeat check
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-heartbeatTicker.C:
					hTime := time.Now()
					outputChan <- &model.LogRecord{
						Timestamp:         hTime,
						ObservedTimestamp: hTime,
						SeverityText:      "INFO",
						SeverityNumber:    model.OTelSeverityNumber("INFO"),
						Body:              "LegacyTel Standard 24-Hour Heartbeat Status Check - Pipeline remains active and healthy.",
						Attributes: map[string]interface{}{
							"legacy.user_code": "SS05",
							"system.status":    "ACTIVE",
						},
						Resource: map[string]interface{}{
							"host.name": "localhost",
							"os.type":   "system",
						},
					}
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case lr, ok := <-outputChan:
				if !ok {
					return
				}

				// Standardize, enrich taxonomy codes, increment statistical telemetry
				stats.ProcessAndEnrich(lr)

				// Export to active SIEMs (Splunk / OTLP)
				exporterMgr.Submit(lr)

				// Push to live visual dashboard client streams
				if dashServer != nil {
					dashServer.Broadcast(lr)
				}
			}
		}
	}()

	log.Println("[INFO] LegacyTel Agent successfully initialized and running.")

	// 7. Graceful Shutdown listener
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan

	log.Printf("[INFO] Received system signal %v. Cleaning up pipeline...\n", sig)
	cancel() // Cancel context to signal receivers to stop

	// Stop receivers
	for _, rec := range receivers {
		log.Printf("[INFO] Stopping receiver: %s\n", rec.GetName())
		_ = rec.Stop()
	}

	// Close pipeline channels
	close(outputChan)
	
	// Exporter manager stops automatically via defer close

	// Give a split second for final flushes
	time.Sleep(500 * time.Millisecond)
	log.Println("[INFO] LegacyTel Agent successfully stopped. Safe operation completed.")
}
