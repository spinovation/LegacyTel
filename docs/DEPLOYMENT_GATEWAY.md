# LegacyTel: Collector Gateway & SIEM Deployment Guide

This document is a standalone, self-contained deployment manual for **Infrastructure Engineers**, **SIEM Architects**, and **Security Analysts**. It details how to install and configure the centralized **LegacyTel Collector Gateway** (the Go-based collector agent), establish secure Mutual TLS (mTLS) receivers, and route normalized events downstream to **any SIEM** or **Cribl Stream**.

---

## 1. Gateway Installation (The Collector)

The LegacyTel Gateway runs on a centralized Linux server (RedHat, SUSE, Debian) or directly on legacy partitions.

### Step 1: Compile the Binary
1. Clone or copy the LegacyTel source files into `/opt/legacytel`.
2. Compile the zero-dependency Go binary:
   ```bash
   go build -o legacytel cmd/agent/main.go
   ```

### Step 2: Establish Certificate Credentials
1. Move the compiled binary to `/usr/local/bin`:
   ```bash
   sudo cp legacytel /usr/local/bin/
   ```
2. Create directories for files and certs:
   ```bash
   sudo mkdir -p /etc/legacytel/certs
   sudo mkdir -p /var/log/legacytel
   ```
3. Copy your configurations and generated certificates (from the `certs` folder):
   ```bash
   sudo cp config.yaml /etc/legacytel/
   sudo cp certs/server.crt /etc/legacytel/certs/
   sudo cp certs/server.key /etc/legacytel/certs/
   sudo cp certs/root_ca.crt /etc/legacytel/certs/
   ```
4. Set secure file permissions:
   ```bash
   sudo chmod 600 /etc/legacytel/certs/server.key
   sudo chmod 644 /etc/legacytel/certs/server.crt
   sudo chmod 644 /etc/legacytel/certs/root_ca.crt
   ```

### Step 3: Edit configuration (`/etc/legacytel/config.yaml`)
Enable TLS and mTLS (Mutual TLS) under the receivers block:

```yaml
receivers:
  zos_smf:
    enabled: true
    bind_address: "0.0.0.0"
    port: 5080
    format: "binary"
    charset: "ebcdic"
    tls_enabled: true
    cert_file: "/etc/legacytel/certs/server.crt"
    key_file: "/etc/legacytel/certs/server.key"
    client_ca_file: "/etc/legacytel/certs/root_ca.crt" # Enables mTLS
```

### Step 4: Configure Background Daemon (Systemd)
1. Create `/etc/systemd/system/legacytel.service`:
   ```ini
   [Unit]
   Description=LegacyTel Mainframe Log Observability Agent
   After=network.target

   [Service]
   Type=simple
   User=root
   WorkingDirectory=/etc/legacytel
   ExecStart=/usr/local/bin/legacytel -config /etc/legacytel/config.yaml -assets /opt/legacytel/pkg/dashboard/assets
   Restart=on-failure
   RestartSec=5s
   StandardOutput=journal
   StandardError=journal

   [Install]
   WantedBy=multi-user.target
   ```
2. Reload and start the service:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable legacytel
   sudo systemctl start legacytel
   ```

---

## 2. Downstream SIEM & Cribl Integrations

LegacyTel is SIEM-neutral. You can configure OTLP/HTTP (for modern SIEMs) or Syslog/CEF/LEEF (for legacy SIEMs).

### A. Microsoft Sentinel & Modern SIEMs (OTLP):
Modern SIEMs (Datadog, Elastic, Google Chronicle, Microsoft Sentinel) ingest OTLP logs directly. In `/etc/legacytel/config.yaml`, configure `otlp_http`:
```yaml
exporters:
  otlp_http:
    enabled: true
    endpoint: "https://<siem-otlp-endpoint-url>/v1/logs"
```

### B. Legacy SIEMs (Syslog / CEF / LEEF):
To stream standard structured text over TCP (e.g. port `514`) for **IBM QRadar**, **ArcSight**, or **LogRhythm**:
```yaml
exporters:
  syslog:
    enabled: true
    network: "tcp"
    endpoint: "siem-syslog-collector:514"
    format: "cef" # Pick: 'cef' (ArcSight), 'leef' (QRadar), or 'rfc5424' (Standard)
```

### C. Cribl Stream Ingestion:
1. In Cribl Stream UI, go to **Sources** > **OpenTelemetry** > **New Source** (`port 4318`).
2. Point LegacyTel's `config.yaml` `otlp_http` to your Cribl worker node.
3. Configure a **Cribl Eval Function** to flatten nested OTel fields (such as `resourceLogs[0].resource.attributes`) into root-level keys (`platform`, `host`, `user_code`) and purge original nested blocks to save **45% data weight**.
4. Configure a **Drop Function** to filter out normal `SS05` daily heartbeat logs at the Cribl layer before they hit the SIEM, saving massive index license costs.
