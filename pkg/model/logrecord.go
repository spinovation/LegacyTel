package model

import (
	"encoding/json"
	"time"
)

// LogRecord represents the internal data structure fully aligned with the
// OpenTelemetry Log Record Specification (https://opentelemetry.io/docs/specs/otel/logs/data-model/).
type LogRecord struct {
	Timestamp         time.Time              `json:"timestamp"`          // Time when the event occurred
	ObservedTimestamp time.Time              `json:"observed_timestamp"` // Time when the collector ingested the event
	SeverityText      string                 `json:"severity_text"`      // E.g., INFO, WARN, ERROR, FATAL
	SeverityNumber    int32                  `json:"severity_number"`    // 1-24 mapping to OTel spec
	Body              string                 `json:"body"`               // Human readable event message
	Attributes        map[string]interface{} `json:"attributes"`         // Specific event attributes
	Resource          map[string]interface{} `json:"resource"`           // Resource attributes describing the source system
}

// OTelSeverityNumber maps standard severity texts to OpenTelemetry severity numbers.
func OTelSeverityNumber(severity string) int32 {
	switch severity {
	case "TRACE":
		return 1
	case "DEBUG":
		return 5
	case "INFO":
		return 9
	case "WARN":
		return 13
	case "ERROR":
		return 17
	case "FATAL":
		return 21
	default:
		return 9 // Default to INFO
	}
}

// ToOTLPJSON serializes the LogRecord to the official OTLP JSON logs format.
func (lr *LogRecord) ToOTLPJSON() ([]byte, error) {
	// Represents OTLP ExportLogsServiceRequest
	type KeyValue struct {
		Key   string      `json:"key"`
		Value interface{} `json:"value"`
	}

	type OTelLogRecord struct {
		TimeUnixNano           int64      `json:"timeUnixNano"`
		ObservedTimeUnixNano   int64      `json:"observedTimeUnixNano"`
		SeverityNumber         int32      `json:"severityNumber"`
		SeverityText           string     `json:"severityText"`
		Body                   struct {
			StringValue string `json:"stringValue"`
		} `json:"body"`
		Attributes             []KeyValue `json:"attributes"`
	}

	type ScopeLogs struct {
		Scope struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"scope"`
		LogRecords []OTelLogRecord `json:"logRecords"`
	}

	type ResourceLogs struct {
		Resource struct {
			Attributes []KeyValue `json:"attributes"`
		} `json:"resource"`
		ScopeLogs []ScopeLogs `json:"scopeLogs"`
	}

	type OTLPPayload struct {
		ResourceLogs []ResourceLogs `json:"resourceLogs"`
	}

	// Build key-values
	var resAttrs []KeyValue
	for k, v := range lr.Resource {
		resAttrs = append(resAttrs, KeyValue{Key: k, Value: v})
	}

	var logAttrs []KeyValue
	for k, v := range lr.Attributes {
		logAttrs = append(logAttrs, KeyValue{Key: k, Value: v})
	}

	otRecord := OTelLogRecord{
		TimeUnixNano:         lr.Timestamp.UnixNano(),
		ObservedTimeUnixNano: lr.ObservedTimestamp.UnixNano(),
		SeverityNumber:       lr.SeverityNumber,
		SeverityText:         lr.SeverityText,
		Attributes:           logAttrs,
	}
	otRecord.Body.StringValue = lr.Body

	scopeLog := ScopeLogs{
		LogRecords: []OTelLogRecord{otRecord},
	}
	scopeLog.Scope.Name = "legacytel-agent"
	scopeLog.Scope.Version = "1.0.0"

	resLog := ResourceLogs{
		ScopeLogs: []ScopeLogs{scopeLog},
	}
	resLog.Resource.Attributes = resAttrs

	payload := OTLPPayload{
		ResourceLogs: []ResourceLogs{resLog},
	}

	return json.Marshal(payload)
}

// ToSplunkHECJSON formats the LogRecord to a Splunk HEC event format.
func (lr *LogRecord) ToSplunkHECJSON(token, index, source, sourcetype string) ([]byte, error) {
	// Splunk HEC structure expects a JSON payload containing:
	// - time: epoch time in seconds (float)
	// - host: source hostname
	// - source: event source
	// - sourcetype: event sourcetype
	// - index: target Splunk index
	// - event: actual body or a structured JSON map
	
	host := "unknown-host"
	if h, ok := lr.Resource["host.name"].(string); ok {
		host = h
	}

	// Package the full OTel schema inside the Splunk event so SIEM analysts have full visibility
	type SplunkHECPayload struct {
		Time       float64     `json:"time"`
		Host       string      `json:"host"`
		Source     string      `json:"source"`
		Sourcetype string      `json:"sourcetype"`
		Index      string      `json:"index"`
		Event      *LogRecord  `json:"event"`
	}

	payload := SplunkHECPayload{
		Time:       float64(lr.Timestamp.UnixNano()) / 1e9,
		Host:       host,
		Source:     source,
		Sourcetype: sourcetype,
		Index:      index,
		Event:      lr,
	}

	return json.Marshal(payload)
}
