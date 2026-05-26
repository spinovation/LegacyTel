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

// QAUDJRNReceiver ingests and decodes QAUDJRN Security Audit entries from IBM i / AS400.
type QAUDJRNReceiver struct {
	settings config.ReceiverSettings
	mu       sync.Mutex
	running  bool
	stopChan chan struct{}
}

func NewQAUDJRNReceiver(settings config.ReceiverSettings) *QAUDJRNReceiver {
	return &QAUDJRNReceiver{
		settings: settings,
		stopChan: make(chan struct{}),
	}
}

func (r *QAUDJRNReceiver) GetName() string {
	return "IBM i QAUDJRN Receiver"
}

func (r *QAUDJRNReceiver) Start(ctx context.Context, outputChan chan<- *model.LogRecord) error {
	r.mu.Lock()
	r.running = true
	r.mu.Unlock()

	go r.runSimulator(outputChan)

	return nil
}

func (r *QAUDJRNReceiver) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		close(r.stopChan)
		r.running = false
	}
	return nil
}

// runSimulator generates AS/400 QAUDJRN entries mapping to the audit journal type structure.
func (r *QAUDJRNReceiver) runSimulator(outputChan chan<- *model.LogRecord) {
	ticker := time.NewTicker(600 * time.Millisecond)
	defer ticker.Stop()

	profiles := []string{"QSECOFR", "QSYSOPR", "QUSER", "ACCTMGR", "DEVLEAD"}
	subsystems := []string{"QINTER", "QBATCH", "QSERVER", "QSYSWRK"}
	programs := []string{"QCMD", "QSECSYS", "INVENTRY", "GL010", "PR_JOB"}

	for {
		select {
		case <-r.stopChan:
			return
		case <-ticker.C:
			// Pick a QAUDJRN entry type
			// PW - Password errors
			// AF - Authority Failures
			// CP - Profile changes
			// SV - System Value changes
			// JS - Job Session Events
			entryTypes := []string{"PW", "AF", "CP", "SV", "JS"}
			entryType := entryTypes[rand.Intn(len(entryTypes))]

			now := time.Now()
			profile := profiles[rand.Intn(len(profiles))]
			subsys := subsystems[rand.Intn(len(subsystems))]
			prog := programs[rand.Intn(len(programs))]
			jobName := fmt.Sprintf("%06d/%s/QPADEV%04d", rand.Intn(999999), profile, rand.Intn(999))

			var lr *model.LogRecord
			attrs := make(map[string]interface{})
			attrs["qaudjrn.journal_code"] = "T" // 'T' stands for Audit Journal
			attrs["qaudjrn.entry_type"] = entryType
			attrs["qaudjrn.job_name"] = jobName
			attrs["qaudjrn.current_user"] = profile
			attrs["qaudjrn.program_name"] = prog
			attrs["qaudjrn.sequence_number"] = rand.Int63n(10000000)
			attrs["security.user"] = profile

			var body string
			var severity string

			switch entryType {
			case "PW": // Password Validation Errors
				subEvent := rand.Intn(3)
				severity = "WARN"
				switch subEvent {
				case 0: // Invalid password entered
					body = fmt.Sprintf("QAUDJRN entry: PW - Invalid password entered by user %s on job %s.", profile, jobName)
					attrs["legacy.user_code"] = "LL03"
					attrs["security.action"] = "INVALID_PASSWORD"
				case 1: // User login success (simulated)
					body = fmt.Sprintf("QAUDJRN entry: PW - Successful login validation completed for user profile %s.", profile)
					severity = "INFO"
					attrs["legacy.user_code"] = "LL01"
					attrs["security.action"] = "LOGIN_SUCCESS"
				case 2: // Password reset success
					body = fmt.Sprintf("QAUDJRN entry: PW - Password changed successfully for user profile %s by administrator.", profile)
					severity = "INFO"
					attrs["legacy.user_code"] = "SA07"
					attrs["security.action"] = "PASSWORD_RESET"
				}

			case "AF": // Authority Failures
				severity = "ERROR"
				body = fmt.Sprintf("QAUDJRN entry: AF - Authority failure. User %s lacked *USE authority to object QGPL/DBTABLE in library QGPL. Program %s.", profile, prog)
				attrs["legacy.user_code"] = "PA02"
				attrs["security.action"] = "AUTHORITY_FAILURE"
				attrs["object.name"] = "DBTABLE"
				attrs["object.library"] = "QGPL"
				attrs["object.type"] = "*FILE"

			case "CP": // Change Profile
				subEvent := rand.Intn(3)
				severity = "INFO"
				targetUser := profiles[rand.Intn(len(profiles))]
				switch subEvent {
				case 0: // Create User
					body = fmt.Sprintf("QAUDJRN entry: CP - User profile %s created by administrator profile %s.", targetUser, profile)
					attrs["legacy.user_code"] = "SA01"
					attrs["security.action"] = "USER_CREATED"
					attrs["security.target_user"] = targetUser
				case 1: // Modify User
					body = fmt.Sprintf("QAUDJRN entry: CP - User profile %s updated: Special Authorities (*ALLOBJ) changed by profile %s.", targetUser, profile)
					severity = "WARN"
					attrs["legacy.user_code"] = "SA02"
					attrs["security.action"] = "USER_MODIFIED"
					attrs["security.target_user"] = targetUser
				case 2: // Delete User
					body = fmt.Sprintf("QAUDJRN entry: CP - User profile %s deleted by profile %s.", targetUser, profile)
					attrs["legacy.user_code"] = "SA03"
					attrs["security.action"] = "USER_DELETED"
					attrs["security.target_user"] = targetUser
				}

			case "SV": // System Value Changes
				severity = "WARN"
				body = fmt.Sprintf("QAUDJRN entry: SV - System Value QSECURITY (Security Level) changed from '40' to '50' by user %s.", profile)
				attrs["legacy.user_code"] = "CC02"
				attrs["security.action"] = "SECURITY_CONFIG_CHANGED"
				attrs["system.value"] = "QSECURITY"
				attrs["system.old_value"] = "40"
				attrs["system.new_value"] = "50"

			case "JS": // Job Session Actions
				subEvent := rand.Intn(2)
				severity = "INFO"
				switch subEvent {
				case 0: // Job Started
					body = fmt.Sprintf("QAUDJRN entry: JS - Job %s started in subsystem %s.", jobName, subsys)
					attrs["legacy.user_code"] = "SS01"
					attrs["job.status"] = "STARTED"
					attrs["job.subsystem"] = subsys
				case 1: // Job Stopped
					body = fmt.Sprintf("QAUDJRN entry: JS - Job %s completed normally. CPU seconds used: 2.45.", jobName)
					attrs["legacy.user_code"] = "SS02"
					attrs["job.status"] = "STOPPED"
					attrs["job.cpu_time"] = 2.45
				}
			}

			lr = &model.LogRecord{
				Timestamp:         now,
				ObservedTimestamp: now,
				SeverityText:      severity,
				SeverityNumber:    model.OTelSeverityNumber(severity),
				Body:              body,
				Attributes:        attrs,
				Resource: map[string]interface{}{
					"host.name":              "AS400-ISERIES-DB01",
					"os.type":                "ibm_i",
					"device.id":              "LPAR12",
					"partition.id":           12,
					"telemetry.sdk.language": "go",
				},
			}

			outputChan <- lr
		}
	}
}
