package receiver

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"legacytel/pkg/config"
	"legacytel/pkg/model"
)

// EBCDIC to ASCII translation table (Code Page 1047 standard)
var ebcdicToAsciiMap = [256]byte{
	0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
	0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F,
	0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E, 0x2F,
	0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x3D, 0x3E, 0x3F,
	0x20, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x5B, 0x2E, 0x3C, 0x28, 0x2B, 0x21,
	0x26, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x24, 0x2A, 0x29, 0x3B, 0x5E, 0x2D,
	0x2F, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x7C, 0x2C, 0x25, 0x5F, 0x3E, 0x3F,
	0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x60, 0x3A, 0x23, 0x40, 0x27, 0x3D, 0x22,
	0xFF, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	0xFF, 0x6A, 0x6B, 0x6C, 0x6D, 0x6E, 0x6F, 0x70, 0x71, 0x72, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	0xFF, 0x7E, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7A, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	0x7B, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	0x7D, 0x4A, 0x4B, 0x4C, 0x4D, 0x4E, 0x4F, 0x50, 0x51, 0x52, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	0x5C, 0xFF, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5A, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
}

// EBCDICBytesToASCII converts an EBCDIC-encoded byte slice to standard ASCII.
func EBCDICBytesToASCII(ebcdicBytes []byte) string {
	asciiBytes := make([]byte, len(ebcdicBytes))
	for i, b := range ebcdicBytes {
		asciiBytes[i] = ebcdicToAsciiMap[b]
	}
	return string(asciiBytes)
}

// SMFReceiver ingests and processes z/OS System Management Facility logs.
type SMFReceiver struct {
	settings config.ReceiverSettings
	mu       sync.Mutex
	running  bool
	stopChan chan struct{}
}

func NewSMFReceiver(settings config.ReceiverSettings) *SMFReceiver {
	return &SMFReceiver{
		settings: settings,
		stopChan: make(chan struct{}),
	}
}

func (r *SMFReceiver) GetName() string {
	return "z/OS SMF Receiver"
}

func (r *SMFReceiver) Start(ctx context.Context, outputChan chan<- *model.LogRecord) error {
	r.mu.Lock()
	r.running = true
	r.mu.Unlock()

	go r.runSimulator(outputChan)

	return nil
}

func (r *SMFReceiver) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		close(r.stopChan)
		r.running = false
	}
	return nil
}

// runSimulator generates high-fidelity SMF records simulating mainframe audits
func (r *SMFReceiver) runSimulator(outputChan chan<- *model.LogRecord) {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()

	users := []string{"SYSADMIN", "DB2USER", "SECADM", "CICSUSER", "OPERATOR"}
	jobs := []string{"STCDB2", "STCCICS", "BATCH01", "PAYROLL", "RACFINIT"}
	terminals := []string{"L3270A1", "L3270B2", "SYSCON", "VTERM01"}

	for {
		select {
		case <-r.stopChan:
			return
		case <-ticker.C:
			// Pick a random record type
			// SMF 80: RACF Security
			// SMF 30: Job Workload
			// SMF 90: Operator Actions
			recType := rand.Intn(3)
			now := time.Now()

			var lr *model.LogRecord

			switch recType {
			case 0: // SMF 80 (RACF Security)
				user := users[rand.Intn(len(users))]
				terminal := terminals[rand.Intn(len(terminals))]
				job := jobs[rand.Intn(len(jobs))]

				// Determine sub-event: Login success, failure, privilege escalation, etc.
				subEvent := rand.Intn(4)
				var body string
				var severity string
				attrs := make(map[string]interface{})
				attrs["smf.record_type"] = 80
				attrs["smf.subtype"] = 1
				attrs["smf.job_name"] = job
				attrs["smf.terminal"] = terminal
				attrs["security.user"] = user

				switch subEvent {
				case 0: // Login Success
					body = fmt.Sprintf("ICH70001I USERID %s ACTIVE. LAST ACCESS AT %s ON TERMINAL %s", user, now.Add(-24*time.Hour).Format("15:04:05"), terminal)
					severity = "INFO"
					attrs["legacy.user_code"] = "LL01"
					attrs["security.action"] = "LOGIN_SUCCESS"
				case 1: // Login Failure (RACF check fail)
					body = fmt.Sprintf("ICH408I USERID %s TERMINAL %s SUBGROUP DEPT01 - PASSWORD INVALID", user, terminal)
					severity = "WARN"
					attrs["legacy.user_code"] = "LL03"
					attrs["security.action"] = "LOGIN_FAILURE"
				case 2: // Privilege Check (Success)
					body = fmt.Sprintf("ICH70002I USERID %s AUTHORIZED TO RESOURCE FACILITY.SUPERUSER BY RACF", user)
					severity = "INFO"
					attrs["legacy.user_code"] = "PA01"
					attrs["security.action"] = "PRIVILEGED_ACCESS_GRANTED"
				case 3: // Privilege Check (Failure)
					body = fmt.Sprintf("ICH408I USERID %s INSUFFICIENT ACCESS AUTHORITY TO RESOURCE SYS1.PARMLIB", user)
					severity = "ERROR"
					attrs["legacy.user_code"] = "PA02"
					attrs["security.action"] = "PRIVILEGED_ACCESS_DENIED"
				}

				lr = &model.LogRecord{
					Timestamp:         now,
					ObservedTimestamp: now,
					SeverityText:      severity,
					SeverityNumber:    model.OTelSeverityNumber(severity),
					Body:              body,
					Attributes:        attrs,
					Resource: map[string]interface{}{
						"host.name":              "ZOS-MAINFRAME-IBM390",
						"os.type":                "zos",
						"device.id":              "SYS1",
						"sysplex.name":           "PLEX1",
						"telemetry.sdk.language": "go",
					},
				}

			case 1: // SMF 30 (Job Workload Account)
				job := jobs[rand.Intn(len(jobs))]
				subEvent := rand.Intn(4)
				var body string
				var severity string
				attrs := make(map[string]interface{})
				attrs["smf.record_type"] = 30
				attrs["smf.job_name"] = job
				attrs["smf.job_id"] = fmt.Sprintf("JOB%05d", rand.Intn(99999))

				switch subEvent {
				case 0: // Job Started
					body = fmt.Sprintf("IEF403I %s - STARTED - TIME=%s", job, now.Format("15.04.05"))
					severity = "INFO"
					attrs["legacy.user_code"] = "SS01"
					attrs["job.status"] = "STARTED"
				case 1: // Job Ended
					body = fmt.Sprintf("IEF404I %s - ENDED - TIME=%s - COND CODE 0000", job, now.Format("15.04.05"))
					severity = "INFO"
					attrs["legacy.user_code"] = "SS02"
					attrs["job.status"] = "STOPPED"
				case 2: // Utilization Threshold Reached
					body = fmt.Sprintf("IEF085I %s - UTILIZATION THRESHOLD REACHED: CPU USAGE AT 94%%", job)
					severity = "WARN"
					attrs["legacy.user_code"] = "CM02"
					attrs["job.cpu_pct"] = 94
				case 3: // Memory change / Limit Check
					body = fmt.Sprintf("IEF196I %s - MEMORY STEP EXTENDED TO REGION SIZE 8192K BY USER REQUEST", job)
					severity = "INFO"
					attrs["legacy.user_code"] = "CM04"
					attrs["job.memory_allocated"] = "8192K"
				}

				lr = &model.LogRecord{
					Timestamp:         now,
					ObservedTimestamp: now,
					SeverityText:      severity,
					SeverityNumber:    model.OTelSeverityNumber(severity),
					Body:              body,
					Attributes:        attrs,
					Resource: map[string]interface{}{
						"host.name":    "ZOS-MAINFRAME-IBM390",
						"os.type":      "zos",
						"device.id":    "SYS1",
						"sysplex.name": "PLEX1",
					},
				}

			case 2: // SMF 90 (Operator Commands & System Actions)
				subEvent := rand.Intn(3)
				var body string
				var severity string
				attrs := make(map[string]interface{})
				attrs["smf.record_type"] = 90

				switch subEvent {
				case 0: // Config Change
					body = "IEE252I MEMBER CEEDOPT PRN IN SYS1.PARMLIB HAS BEEN UPDATED BY SYSADMIN"
					severity = "INFO"
					attrs["legacy.user_code"] = "CC01"
					attrs["sys.config_member"] = "CEEDOPT"
				case 1: // Security Config Change
					body = "ICH102I RACF SECURITY RULES REFRESHED FOR CLASS FACILITY BY OPERATOR"
					severity = "WARN"
					attrs["legacy.user_code"] = "CC02"
					attrs["security.class"] = "FACILITY"
				case 2: // System Clock Change / Sequencing
					body = "IEE136I LOCAL TIME CHANGED - SYSTEM CLOCK SEQUENCING COMPLETED"
					severity = "INFO"
					attrs["legacy.user_code"] = "CC01"
				}

				lr = &model.LogRecord{
					Timestamp:         now,
					ObservedTimestamp: now,
					SeverityText:      severity,
					SeverityNumber:    model.OTelSeverityNumber(severity),
					Body:              body,
					Attributes:        attrs,
					Resource: map[string]interface{}{
						"host.name":    "ZOS-MAINFRAME-IBM390",
						"os.type":      "zos",
						"device.id":    "SYS1",
						"sysplex.name": "PLEX1",
					},
				}
			}

			if lr != nil {
				outputChan <- lr
			}
		}
	}
}
