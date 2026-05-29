package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"legacytel/pkg/classifier"
	"legacytel/pkg/inventory"
)

func main() {
	log.Printf("[WORKER] LegacyTel Active Worker Process [%s] started. PID: %d", "v2.0.0", os.Getpid())

	// 1. Programmatic Hypervisor Classification
	hvType, hvName := classifier.DetectHypervisor()
	log.Printf("[WORKER] Native Hypervisor Classifier: Classified Host as %s (%s)", hvType, hvName)

	// 2. Unprivileged Application Inventory Scanning
	apps := inventory.ScanApplications()
	log.Printf("[WORKER] Scanned Application Inventory: %v", apps)

	// 3. Unix Socket Handoff Recovery (FD 3)
	var listener net.Listener
	var err error

	if runtime.GOOS != "windows" {
		// File descriptor 3 is standard for passed files (e.g., cmd.ExtraFiles[0])
		file := os.NewFile(3, "supervisor-listener")
		if file != nil {
			listener, err = net.FileListener(file)
			if err == nil {
				log.Println("[WORKER] Socket handoff successful: Inherited listening socket from Supervisor on FD 3.")
				defer listener.Close()
				go handleIncomingStreams(listener)
			}
		}
	}

	// 4. Local fallback or secondary listener if no socket was passed
	if listener == nil {
		listener, err = net.Listen("tcp", "127.0.0.1:0") // Bind to random port to verify functionality
		if err != nil {
			log.Printf("[WORKER] Fallback listener failed: %v", err)
		} else {
			log.Printf("[WORKER] Running status port on: %s", listener.Addr().String())
			defer listener.Close()
			go handleIncomingStreams(listener)
		}
	}

	// 5. Active Telemetry Pipeline Simulation (Enriched OTel LogRecords)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runTelemetryPipeline(ctx, hvType, hvName, apps)

	// Handle Graceful Termination
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("[WORKER] Graceful shutdown initiated. Flushing log buffers...")
	time.Sleep(500 * time.Millisecond)
	log.Println("[WORKER] Worker terminated successfully.")
}

func handleIncomingStreams(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			_, _ = c.Write([]byte("LegacyTel v2.0.0 Active Worker Status: OK\n"))
		}(conn)
	}
}

func runTelemetryPipeline(ctx context.Context, hvType string, hvName string, apps []string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Print structured telemetry showing hypervisor and inventory tags
			fmt.Printf("[TELEMETRY] [OTel LogRecord] Timestamp: %s | Severity: INFO | Body: System health check OK.\n", time.Now().Format(time.RFC3339))
			fmt.Printf("   ├─ Resource Attributes: {host.name: %s, os.type: %s, hypervisor.type: %s, hypervisor.name: %s}\n", 
				getHostName(), runtime.GOOS, hvType, hvName)
			fmt.Printf("   └─ Scanned Inventory: {host.inventory.apps: %v}\n", apps)
		}
	}
}

func getHostName() string {
	h, _ := os.Hostname()
	return h
}
