package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Supervisor settings
const (
	ControlPlaneURL = "http://localhost:9090"
	HeartbeatSec    = 3
	NodeIDFile      = "node_id.txt"
)

type SupervisorState struct {
	mu            sync.Mutex
	NodeID        string
	Hostname      string
	OS            string
	Version       string
	CurrentWorker *exec.Cmd
	ActiveSocket  net.Listener
	Upgrading     bool
}

var state = &SupervisorState{
	OS:      runtime.GOOS,
	Version: "v1.9.8", // Current baseline version of our agent
}

func main() {
	log.Println("[SUPERVISOR] LegacyTel Service Shell starting...")

	// 1. Resolve Node Identity
	state.Hostname, _ = os.Hostname()
	state.NodeID = resolveNodeID()
	log.Printf("[SUPERVISOR] Node Registered ID: %s on host %s", state.NodeID, state.Hostname)

	// 2. Open Network Listener Sockets (Supervisor acts as the Socket Holder)
	// We hold open port 5080 (mainframe SMF mock interface) to share with the worker
	var err error
	state.ActiveSocket, err = net.Listen("tcp", "127.0.0.1:5080")
	if err != nil {
		log.Printf("[SUPERVISOR] Failed to bind to primary socket: %v. Fallback to worker self-bind.", err)
	} else {
		log.Printf("[SUPERVISOR] Primary socket successfully bound to 127.0.0.1:5080. Holding file descriptor.")
	}

	// 3. Spawn Initial Worker Process
	go startWorkerProcess()

	// 4. Register and Heartbeat Loop to Central Control Plane
	go runHeartbeatLoop()

	// Keep main goroutine alive
	select {}
}

func resolveNodeID() string {
	data, err := os.ReadFile(NodeIDFile)
	if err == nil {
		return string(bytes.TrimSpace(data))
	}
	// Generate new unique Node ID
	id := fmt.Sprintf("node-%s-%d", state.OS, time.Now().UnixNano()%100000)
	_ = os.WriteFile(NodeIDFile, []byte(id), 0644)
	return id
}

// startWorkerProcess launches or restarts the worker child process
func startWorkerProcess() {
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.CurrentWorker != nil && state.CurrentWorker.Process != nil {
		_ = state.CurrentWorker.Process.Kill()
	}

	// Build the path to the worker executable
	workerName := "legacytel-worker"
	if runtime.GOOS == "windows" {
		workerName = "legacytel-worker.exe"
	}
	
	dir, _ := os.Getwd()
	workerPath := filepath.Join(dir, workerName)

	// In standard deployments, the worker is pre-compiled. If missing, we simulate execution
	if _, err := os.Stat(workerPath); os.IsNotExist(err) {
		log.Printf("[SUPERVISOR] Worker binary not found at %s. Creating a mock worker script for evaluation.", workerPath)
		createMockWorkerBinary(workerPath)
	}

	cmd := exec.Command(workerPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Programmatic Socket-Descriptor Passing (Unix only: Linux, macOS)
	if state.ActiveSocket != nil && runtime.GOOS != "windows" {
		tcpListener, ok := state.ActiveSocket.(*net.TCPListener)
		if ok {
			file, err := tcpListener.File()
			if err == nil {
				// Pass the open TCP socket to the worker as File Descriptor 3
				cmd.ExtraFiles = []*os.File{file}
				log.Println("[SUPERVISOR] Passed open socket file descriptor to worker process.")
			}
		}
	}

	err := cmd.Start()
	if err != nil {
		log.Printf("[SUPERVISOR] [ERROR] Failed to start worker: %v", err)
		return
	}

	state.CurrentWorker = cmd
	log.Printf("[SUPERVISOR] Worker process successfully spawned with PID %d", cmd.Process.Pid)

	// Async wait for process death
	go func(c *exec.Cmd) {
		_ = c.Wait()
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.CurrentWorker == c && !state.Upgrading {
			log.Println("[SUPERVISOR] Worker process exited unexpectedly. Restarting in 2 seconds...")
			time.AfterFunc(2*time.Second, startWorkerProcess)
		}
	}(cmd)
}

// runHeartbeatLoop reports agent stats to control plane and checks for upgrades
func runHeartbeatLoop() {
	ticker := time.NewTicker(HeartbeatSec * time.Second)
	for range ticker.C {
		state.mu.Lock()
		upgrading := state.Upgrading
		state.mu.Unlock()

		if upgrading {
			continue
		}

		payload := map[string]interface{}{
			"id":              state.NodeID,
			"hostname":        state.Hostname,
			"os":              state.OS,
			"version":         state.Version,
			"cpu_usage":       14.2, // Simulated unprivileged metrics
			"memory_usage":    48.5,
			"throughput":      184.2,
			"hypervisor_type": "type-2", // Classified type
			"hypervisor_name": "VirtualBox",
			"app_inventory":   []string{"nginx", "postgresql", "docker"},
		}

		body, _ := json.Marshal(payload)
		resp, err := http.Post(ControlPlaneURL+"/api/v1/heartbeat", "application/json", bytes.NewBuffer(body))
		if err != nil {
			log.Printf("[SUPERVISOR] Heartbeat connection failure to %s: %v", ControlPlaneURL, err)
			continue
		}

		var respData struct {
			TargetVersion    string `json:"target_version"`
			UpgradeScheduled bool   `json:"upgrade_scheduled"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&respData)
		resp.Body.Close()

		if respData.UpgradeScheduled && respData.TargetVersion != state.Version {
			log.Printf("[SUPERVISOR] Pending upgrade detected! Target Version: %s", respData.TargetVersion)
			triggerUpgradeSequence(respData.TargetVersion)
		}
	}
}

// triggerUpgradeSequence coordinates hot-swaps and rollbacks
func triggerUpgradeSequence(targetVer string) {
	state.mu.Lock()
	state.Upgrading = true
	state.mu.Unlock()

	log.Printf("[UPGRADE] Starting upgrade sequence to version %s...", targetVer)

	// 1. Simulate downloading and verifying update package
	time.Sleep(1 * time.Second)
	log.Println("[UPGRADE] Minor version package downloaded successfully.")

	// 2. Backup current worker
	dir, _ := os.Getwd()
	workerName := "legacytel-worker"
	if runtime.GOOS == "windows" {
		workerName = "legacytel-worker.exe"
	}
	workerPath := filepath.Join(dir, workerName)
	backupPath := workerPath + ".bak"

	// Delete old backup if exists
	_ = os.Remove(backupPath)
	err := copyFile(workerPath, backupPath)
	if err != nil {
		log.Printf("[UPGRADE] [FAILED] Failed to create local backup: %v. Aborting.", err)
		state.mu.Lock()
		state.Upgrading = false
		state.mu.Unlock()
		return
	}
	log.Println("[UPGRADE] Backup worker binary saved successfully at legacytel-worker.bak")

	// 3. Atomically overwrite active worker binary
	// In production, we write to a new temp file first and rename it.
	tempPath := workerPath + ".tmp"
	_ = os.Remove(tempPath)
	
	// Create the upgraded binary (simulating minor version upgrade)
	err = createMockWorkerBinaryWithVersion(tempPath, targetVer)
	if err != nil {
		log.Printf("[UPGRADE] [FAILED] Failed to construct upgrade temp: %v", err)
		rollbackUpgrade(workerPath, backupPath)
		return
	}

	// Rename temp binary to actual binary (atomic swap)
	err = os.Rename(tempPath, workerPath)
	if err != nil {
		log.Printf("[UPGRADE] [FAILED] Atomic overwrite failed: %v. Restoring.", err)
		rollbackUpgrade(workerPath, backupPath)
		return
	}
	log.Println("[UPGRADE] Atomic binary overwrite completed successfully.")

	// 4. Terminate old worker and boot upgraded worker
	state.mu.Lock()
	if state.CurrentWorker != nil && state.CurrentWorker.Process != nil {
		log.Printf("[UPGRADE] Terminating current worker process (PID: %d)", state.CurrentWorker.Process.Pid)
		// On Unix, send SIGTERM. On Windows, Kill.
		_ = state.CurrentWorker.Process.Kill()
	}
	state.mu.Unlock()

	// Wait briefly for process to die and ports to clear if not passed
	time.Sleep(500 * time.Millisecond)

	// Spawn new worker
	startWorkerProcess()

	// 5. Health Verification Loop
	log.Println("[UPGRADE] Verifying upgraded worker health...")
	success := verifyWorkerHealth()

	if !success {
		log.Println("[UPGRADE] [ALERT] Upgraded worker failed health verification! Initiating automatic rollback...")
		rollbackUpgrade(workerPath, backupPath)
	} else {
		log.Printf("[UPGRADE] [SUCCESS] LegacyTel successfully upgraded to version %s without reboots!", targetVer)
		state.mu.Lock()
		state.Version = targetVer
		state.Upgrading = false
		state.mu.Unlock()
	}
}

// verifyWorkerHealth monitors the child process for a brief window
func verifyWorkerHealth() bool {
	// Monitor for 5 seconds to check if child stays alive and is healthy
	for i := 0; i < 5; i++ {
		time.Sleep(1 * time.Second)
		state.mu.Lock()
		cmd := state.CurrentWorker
		state.mu.Unlock()

		if cmd == nil || cmd.Process == nil {
			return false
		}

		// Check if process exited
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			log.Printf("[UPGRADE] Upgraded process exited during verification with code: %d", cmd.ProcessState.ExitCode())
			return false
		}
	}
	return true
}

func rollbackUpgrade(workerPath, backupPath string) {
	log.Println("[ROLLBACK] Restoring previous stable worker from backup...")

	// Terminate any broken running child worker
	state.mu.Lock()
	if state.CurrentWorker != nil && state.CurrentWorker.Process != nil {
		_ = state.CurrentWorker.Process.Kill()
	}
	state.mu.Unlock()

	time.Sleep(500 * time.Millisecond)

	// Restore original binary
	_ = os.Remove(workerPath)
	err := os.Rename(backupPath, workerPath)
	if err != nil {
		log.Printf("[ROLLBACK] [FATAL] Rollback failed to restore backup file: %v", err)
	} else {
		log.Println("[ROLLBACK] Backup binary successfully restored. Restarting original worker...")
	}

	// Restart original stable worker
	state.mu.Lock()
	state.Upgrading = false
	state.mu.Unlock()
	
	startWorkerProcess()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func createMockWorkerBinary(path string) {
	_ = createMockWorkerBinaryWithVersion(path, "v1.9.8")
}

func createMockWorkerBinaryWithVersion(path string, ver string) error {
	// We write a tiny self-contained Go script that compiles or runs as a mock worker
	// To make this fully functional, we can write a simple Go source code text block 
	// and compile it dynamically or compile the actual worker!
	// For immediate test verification, we write a Go file and execute "go build" to build it!
	
	srcPath := path + ".go"
	srcCode := fmt.Sprintf(`package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"time"
)

func main() {
	log.Printf("[WORKER] LegacyTel Active Worker Process [%s] started. PID: %%d", os.Getpid())

	// If a file descriptor is passed (FD 3), reconstruct listener
	var listener net.Listener
	var err error

	if len(os.Args) > 0 && os.Getenv("LISTEN_FD") != "" {
		// Mock descriptor reconstruction
		log.Println("[WORKER] Reconstructed socket listener from FD 3 successfully.")
	}

	// Listen on dummy port if descriptor passing fallback is needed
	if listener == nil {
		listener, err = net.Listen("tcp", "127.0.0.1:0") // Random ephemeral port for worker metric
		if err == nil {
			defer listener.Close()
			log.Printf("[WORKER] Worker status listener active on %%s", listener.Addr())
		}
	}

	// Keep process alive, simulating active log forwarding
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		fmt.Println("[WORKER] Ingesting & Forwarding OTel Logs... Status: HEALTHY")
	}
}
`, ver)

	err := os.WriteFile(srcPath, []byte(srcCode), 0644)
	if err != nil {
		return err
	}

	// Compile the actual binary on the system!
	cmd := exec.Command("go", "build", "-o", path, srcPath)
	err = cmd.Run()
	_ = os.Remove(srcPath) // Clean up source code file after build
	
	if err != nil {
		return fmt.Errorf("failed to compile worker binary: %w", err)
	}
	return nil
}
