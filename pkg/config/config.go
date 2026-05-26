package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Agent     AgentConfig
	Server    ServerConfig
	Receivers ReceiversConfig
	Exporters ExportersConfig
}

type AgentConfig struct {
	ID          string
	Name        string
	Environment string
	Region      string
}

type ServerConfig struct {
	Host            string
	Port            int
	EnableDashboard bool
}

type ReceiversConfig struct {
	ZOS   ReceiverSettings
	AS400 ReceiverSettings
	EMS   ReceiverSettings
}

type ReceiverSettings struct {
	Enabled      bool
	BindAddress  string
	Port         int
	Format       string
	Charset      string
	TLSEnabled   bool
	CertFile     string
	KeyFile      string
	ClientCAFile string // Mutual TLS (mTLS) client CA certificate file
}

type ExportersConfig struct {
	OTLP   OTLPConfig
	Splunk SplunkConfig
	Syslog SyslogConfig // Added for generic SIEM compatibility (CEF, LEEF, RFC5424)
}

type OTLPConfig struct {
	Enabled   bool
	Endpoint  string
	TimeoutMS int
	Headers   map[string]string
}

type SplunkConfig struct {
	Enabled            bool
	Endpoint           string
	Token              string
	Index              string
	Source             string
	Sourcetype         string
	InsecureSkipVerify bool
}

type SyslogConfig struct {
	Enabled  bool
	Network  string // tcp or udp
	Endpoint string // host:port
	Format   string // rfc5424, cef, or leef
}

// Load parses a simple YAML-like config file with line parsing to maintain standard-library-only builds.
func Load(filepath string) (*Config, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	cfg := &Config{
		Agent: AgentConfig{
			ID:          "legacytel-hq-01",
			Name:        "mainframe-observability-forwarder",
			Environment: "production",
			Region:      "us-east-1",
		},
		Server: ServerConfig{
			Host:            "0.0.0.0",
			Port:            8080,
			EnableDashboard: true,
		},
		Receivers: ReceiversConfig{
			ZOS:   ReceiverSettings{Enabled: true, BindAddress: "0.0.0.0", Port: 5080, Format: "binary", Charset: "ebcdic"},
			AS400: ReceiverSettings{Enabled: true, BindAddress: "0.0.0.0", Port: 5081, Format: "type5", Charset: "ebcdic"},
			EMS:   ReceiverSettings{Enabled: true, BindAddress: "0.0.0.0", Port: 5082, Format: "ems-binary", Charset: "ascii"},
		},
		Exporters: ExportersConfig{
			OTLP: OTLPConfig{
				Enabled:   true,
				Endpoint:  "http://localhost:4318/v1/logs",
				TimeoutMS: 5000,
				Headers:   make(map[string]string),
			},
			Splunk: SplunkConfig{
				Enabled:            false,
				Endpoint:           "http://localhost:8088/services/collector",
				Token:              "splunk-hec-token-1234-5678",
				Index:              "mainframe_security",
				Source:             "legacytel",
				Sourcetype:         "_json",
				InsecureSkipVerify: true,
			},
			Syslog: SyslogConfig{
				Enabled:  true,
				Network:  "tcp",
				Endpoint: "localhost:514",
				Format:   "cef",
			},
		},
	}

	scanner := bufio.NewScanner(file)
	var currentSection string
	var currentSubSection string

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Skip comments and empty lines
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Check for sections (no leading spaces)
		if !strings.HasPrefix(line, " ") && strings.HasSuffix(trimmed, ":") {
			currentSection = strings.TrimSuffix(trimmed, ":")
			currentSubSection = ""
			continue
		}

		// Check for subsections (two leading spaces)
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(trimmed, ":") {
			currentSubSection = strings.TrimSuffix(trimmed, ":")
			continue
		}

		// Parse key-value pairs (four or more leading spaces or two leading spaces if no subsection)
		if strings.Contains(trimmed, ":") {
			parts := strings.SplitN(trimmed, ":", 2)
			key := strings.TrimSpace(parts[0])
			val := strings.Trim(strings.TrimSpace(parts[1]), "\"")

			// Assign values based on section and subsection
			switch currentSection {
			case "agent":
				switch key {
				case "id":
					cfg.Agent.ID = val
				case "name":
					cfg.Agent.Name = val
				case "environment":
					cfg.Agent.Environment = val
				case "region":
					cfg.Agent.Region = val
				}
			case "server":
				switch key {
				case "host":
					cfg.Server.Host = val
				case "port":
					if p, err := strconv.Atoi(val); err == nil {
						cfg.Server.Port = p
					}
				case "enable_dashboard":
					cfg.Server.EnableDashboard = (val == "true")
				}
			case "receivers":
				switch currentSubSection {
				case "zos_smf":
					parseReceiverSettings(&cfg.Receivers.ZOS, key, val)
				case "as400_qaudjrn":
					parseReceiverSettings(&cfg.Receivers.AS400, key, val)
				case "tandem_ems":
					parseReceiverSettings(&cfg.Receivers.EMS, key, val)
				}
			case "exporters":
				switch currentSubSection {
				case "otlp_http":
					switch key {
					case "enabled":
						cfg.Exporters.OTLP.Enabled = (val == "true")
					case "endpoint":
						cfg.Exporters.OTLP.Endpoint = val
					case "timeout_ms":
						if t, err := strconv.Atoi(val); err == nil {
							cfg.Exporters.OTLP.TimeoutMS = t
						}
					}
				case "splunk_hec":
					switch key {
					case "enabled":
						cfg.Exporters.Splunk.Enabled = (val == "true")
					case "endpoint":
						cfg.Exporters.Splunk.Endpoint = val
					case "token":
						cfg.Exporters.Splunk.Token = val
					case "index":
						cfg.Exporters.Splunk.Index = val
					case "source":
						cfg.Exporters.Splunk.Source = val
					case "sourcetype":
						cfg.Exporters.Splunk.Sourcetype = val
					case "insecure_skip_verify":
						cfg.Exporters.Splunk.InsecureSkipVerify = (val == "true")
					}
				case "syslog":
					switch key {
					case "enabled":
						cfg.Exporters.Syslog.Enabled = (val == "true")
					case "network":
						cfg.Exporters.Syslog.Network = val
					case "endpoint":
						cfg.Exporters.Syslog.Endpoint = val
					case "format":
						cfg.Exporters.Syslog.Format = val
					}
				}
			}
		}
	}

	return cfg, nil
}

func parseReceiverSettings(settings *ReceiverSettings, key, val string) {
	switch key {
	case "enabled":
		settings.Enabled = (val == "true")
	case "bind_address":
		settings.BindAddress = val
	case "port":
		if p, err := strconv.Atoi(val); err == nil {
			settings.Port = p
		}
	case "format":
		settings.Format = val
	case "charset":
		settings.Charset = val
	case "tls_enabled":
		settings.TLSEnabled = (val == "true")
	case "cert_file":
		settings.CertFile = val
	case "key_file":
		settings.KeyFile = val
	case "client_ca_file":
		settings.ClientCAFile = val
	}
}
