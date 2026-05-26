package processor

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"legacytel/pkg/model"
)

// Taxonomy descriptions corresponding directly to the user's checklist
var TaxonomyDescriptions = map[string]string{
	"LL01": "Successful login",
	"LL02": "Successful logoff",
	"LL03": "User login failure",
	"LL04": "Password change success",
	"LL05": "Password change failure",
	"CC01": "Application configuration change",
	"CC02": "Security configuration change",
	"PA01": "Successful privilege operation access",
	"PA02": "Failed privileged operation access",
	"SA01": "User creation",
	"SA02": "User change",
	"SA03": "User deletion",
	"SA04": "User profile/role creation",
	"SA05": "User profile/role change",
	"SA06": "User profile/role deletion",
	"SA07": "User password reset",
	"SA08": "User account locked",
	"SA09": "User account unlocked",
	"SS01": "Application started",
	"SS02": "Application stopped",
	"SS03": "Application data dump",
	"SS04": "Application data restore",
	"SS05": "Application logging change",
	"CM01": "Sequencing failure",
	"CM02": "Utilization threshold reached",
	"CM03": "Application code change",
	"CM04": "Application memory change",
}

// TelemetryStats handles atomic tracking of collector performance metrics
type TelemetryStats struct {
	mu             sync.RWMutex
	StartTime      time.Time
	TotalProcessed int64
	ZOSCount       int64
	AS400Count     int64
	EMSCount       int64
	CodeDistribution map[string]int64
	SeverityCount    map[string]int64
	throughputBuffer []int
	alertsLogPath    string // Path to the troubleshooting alerts log history
}

func NewTelemetryStats() *TelemetryStats {
	return &TelemetryStats{
		StartTime:        time.Now(),
		CodeDistribution: make(map[string]int64),
		SeverityCount:    make(map[string]int64),
		throughputBuffer: make([]int, 0),
		alertsLogPath:    "/Users/sridhargs/Documents/Antigravity/MFA/logs/alerts/history.log",
	}
}

// ProcessAndEnrich normalizes log records, maps standard taxonomy, and increments metrics.
func (ts *TelemetryStats) ProcessAndEnrich(lr *model.LogRecord) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.TotalProcessed++

	// 1. Enrich resource attributes
	if lr.Resource == nil {
		lr.Resource = make(map[string]interface{})
	}
	lr.Resource["telemetry.sdk.name"] = "legacytel-agent"
	lr.Resource["telemetry.sdk.version"] = "1.0.0"

	// 2. Classify platform metrics
	platform, _ := lr.Resource["os.type"].(string)
	switch platform {
	case "zos":
		ts.ZOSCount++
	case "ibm_i":
		ts.AS400Count++
	case "nonstop":
		ts.EMSCount++
	}

	// 3. Map Taxonomy Descriptions
	code, ok := lr.Attributes["legacy.user_code"].(string)
	if ok {
		desc, exists := TaxonomyDescriptions[code]
		if exists {
			lr.Attributes["legacy.user_code_description"] = desc
		}
		ts.CodeDistribution[code]++
	} else {
		lr.Attributes["legacy.user_code"] = "UNKNOWN"
		ts.CodeDistribution["UNKNOWN"]++
	}

	// 4. Trace Severity
	ts.SeverityCount[lr.SeverityText]++

	// 5. Trigger local troubleshooting alert if high severity or security event
	if lr.SeverityText == "WARN" || lr.SeverityText == "ERROR" || lr.SeverityText == "FATAL" ||
		code == "LL03" || code == "PA02" || code == "SA08" || code == "CM01" {
		ts.logAlertToFile(lr)
	}
}

// logAlertToFile appends critical events to the local troubleshooting alerts history log
func (ts *TelemetryStats) logAlertToFile(lr *model.LogRecord) {
	// Create directory if missing
	dir := filepath.Dir(ts.alertsLogPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}

	// Open file in append mode
	f, err := os.OpenFile(ts.alertsLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	platform, _ := lr.Resource["os.type"].(string)
	if platform == "" {
		platform = "system"
	}
	code, _ := lr.Attributes["legacy.user_code"].(string)
	desc, _ := lr.Attributes["legacy.user_code_description"].(string)

	timeStr := lr.Timestamp.Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] [%s] [%s] [%s] (%s) -> %s\n", timeStr, lr.SeverityText, platform, code, desc, lr.Body)

	_, _ = f.WriteString(line)
}

// GetSnapshot returns a thread-safe static copy of telemetry stats
func (ts *TelemetryStats) GetSnapshot() map[string]interface{} {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	uptime := time.Since(ts.StartTime).Truncate(time.Second)

	codeDistCopy := make(map[string]int64)
	for k, v := range ts.CodeDistribution {
		codeDistCopy[k] = v
	}

	sevCountCopy := make(map[string]int64)
	for k, v := range ts.SeverityCount {
		sevCountCopy[k] = v
	}

	return map[string]interface{}{
		"uptime_seconds":  int(uptime.Seconds()),
		"total_processed": ts.TotalProcessed,
		"zos_count":       ts.ZOSCount,
		"as400_count":     ts.AS400Count,
		"ems_count":       ts.EMSCount,
		"code_dist":       codeDistCopy,
		"severity_dist":   sevCountCopy,
	}
}
