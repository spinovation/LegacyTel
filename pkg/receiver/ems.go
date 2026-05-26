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

// EMSReceiver ingests and decodes Tandem / HPE NonStop Event Management Service logs.
type EMSReceiver struct {
	settings config.ReceiverSettings
	mu       sync.Mutex
	running  bool
	stopChan chan struct{}
}

func NewEMSReceiver(settings config.ReceiverSettings) *EMSReceiver {
	return &EMSReceiver{
		settings: settings,
		stopChan: make(chan struct{}),
	}
}

func (r *EMSReceiver) GetName() string {
	return "HPE NonStop EMS Receiver"
}

func (r *EMSReceiver) Start(ctx context.Context, outputChan chan<- *model.LogRecord) error {
	r.mu.Lock()
	r.running = true
	r.mu.Unlock()

	go r.runSimulator(outputChan)

	return nil
}

func (r *EMSReceiver) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		close(r.stopChan)
		r.running = false
	}
	return nil
}

// runSimulator generates high-fidelity Tandem NonStop EMS events from major subsystems
func (r *EMSReceiver) runSimulator(outputChan chan<- *model.LogRecord) {
	ticker := time.NewTicker(800 * time.Millisecond)
	defer ticker.Stop()

	subsystems := []string{"TACL", "SAFE", "TMF", "PATHWAY", "TSMP"}
	users := []string{"255,255", "100,5", "10,2", "8,20", "20,1"} // Tandem User IDs (Group, User)

	for {
		select {
		case <-r.stopChan:
			return
		case <-ticker.C:
			subsys := subsystems[rand.Intn(len(subsystems))]
			now := time.Now()
			user := users[rand.Intn(len(users))]
			cpu := rand.Intn(16)
			pin := rand.Intn(2000)
			eventNum := rand.Intn(2000) + 1000

			var lr *model.LogRecord
			attrs := make(map[string]interface{})
			attrs["ems.subsystem_id"] = subsys
			attrs["ems.event_number"] = eventNum
			attrs["ems.cpu"] = cpu
			attrs["ems.pin"] = pin
			attrs["ems.process_id"] = fmt.Sprintf("\\NS1.$Z%02d.PIN%04d", cpu, pin)
			attrs["security.user"] = user

			var body string
			var severity string

			switch subsys {
			case "TACL": // Tandem Advanced Command Language (Operator Session)
				subEvent := rand.Intn(3)
				severity = "INFO"
				switch subEvent {
				case 0:
					body = fmt.Sprintf("TACL Log On: User ID %s registered successfully on port \\NS1.#TERM%02d", user, rand.Intn(20))
					attrs["legacy.user_code"] = "LL01"
					attrs["security.action"] = "LOGIN_SUCCESS"
				case 1:
					body = fmt.Sprintf("TACL Log Off: User ID %s logged off normally from port \\NS1.#TERM%02d", user, rand.Intn(20))
					attrs["legacy.user_code"] = "LL02"
					attrs["security.action"] = "LOGOFF"
				case 2:
					body = fmt.Sprintf("TACL Authentication Failed: Access denied for User ID %s due to invalid password.", user)
					severity = "WARN"
					attrs["legacy.user_code"] = "LL03"
					attrs["security.action"] = "LOGIN_FAILURE"
				}

			case "SAFE": // SecurITy product (Guardian OS Access Control)
				subEvent := rand.Intn(4)
				severity = "WARN"
				targetUser := users[rand.Intn(len(users))]
				switch subEvent {
				case 0: // User Created
					body = fmt.Sprintf("SAFE event: User created. ID %s has registered new user profile %s.", user, targetUser)
					severity = "INFO"
					attrs["legacy.user_code"] = "SA01"
					attrs["security.action"] = "USER_CREATED"
				case 1: // User password reset
					body = fmt.Sprintf("SAFE event: User profile %s password reset completed successfully by ID %s.", targetUser, user)
					severity = "INFO"
					attrs["legacy.user_code"] = "SA07"
					attrs["security.action"] = "PASSWORD_RESET"
				case 2: // User account locked
					body = fmt.Sprintf("SAFE event: User profile %s has been locked due to 3 consecutive authentication failures.", targetUser)
					severity = "ERROR"
					attrs["legacy.user_code"] = "SA08"
					attrs["security.action"] = "USER_LOCKED"
				case 3: // Privileged Command Execution Checked
					body = fmt.Sprintf("SAFE event: Security audit violation. User %s attempted privileged command SFGP on file \\NS1.$DSK0.SECURE.SECFILE. Access Denied.", user)
					severity = "ERROR"
					attrs["legacy.user_code"] = "PA02"
					attrs["security.action"] = "PRIVILEGED_ACCESS_DENIED"
				}

			case "TMF": // Transaction Monitoring Facility
				subEvent := rand.Intn(3)
				severity = "INFO"
				switch subEvent {
				case 0:
					body = fmt.Sprintf("TMF audit: Transaction coordinator started on CPU %02d.", cpu)
					attrs["legacy.user_code"] = "SS01"
					attrs["tmf.action"] = "STARTED"
				case 1:
					body = fmt.Sprintf("TMF audit: Online dump file successfully written for database TMF_DB_01.")
					attrs["legacy.user_code"] = "SS03"
					attrs["tmf.action"] = "DUMP_COMPLETED"
				case 2:
					body = fmt.Sprintf("TMF alert: Transaction sequencing failure on disk volume \\NS1.$DSK02.")
					severity = "ERROR"
					attrs["legacy.user_code"] = "CM01"
					attrs["tmf.action"] = "SEQUENCING_FAILURE"
				}

			case "PATHWAY": // Application Server Transaction manager
				subEvent := rand.Intn(2)
				severity = "INFO"
				switch subEvent {
				case 0:
					body = fmt.Sprintf("Pathway system: Application server class SERVER-INVENTORY-CLASS started successfully.")
					attrs["legacy.user_code"] = "SS01"
				case 1:
					body = fmt.Sprintf("Pathway system: CPU utilization threshold reached. Server class SERVER-INVENTORY-CLASS CPU usage at 91%%.")
					severity = "WARN"
					attrs["legacy.user_code"] = "CM02"
				}

			default: // TSMP (Tandem System Management Processor)
				body = fmt.Sprintf("TSMP event: Disk volume \\NS1.$DSK01 utilization threshold reached: 95%% of capacity used.")
				severity = "WARN"
				attrs["legacy.user_code"] = "CM02"
			}

			lr = &model.LogRecord{
				Timestamp:         now,
				ObservedTimestamp: now,
				SeverityText:      severity,
				SeverityNumber:    model.OTelSeverityNumber(severity),
				Body:              body,
				Attributes:        attrs,
				Resource: map[string]interface{}{
					"host.name":              "TANDEM-NONSTOP-NS1",
					"os.type":                "nonstop",
					"device.id":              "CPU-0",
					"system.name":            "NS1",
					"telemetry.sdk.language": "go",
				},
			}

			outputChan <- lr
		}
	}
}
