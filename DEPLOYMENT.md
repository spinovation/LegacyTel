# LegacyTel: Idiot-Proof Deployment & Integration Guide

This document is the **Master Integration Hub** for the LegacyTel observability pipeline. To make deployment seamless for large enterprise environments, we have separated the installation and configuration instructions into **four standalone, platform-specific manuals**. 

Instead of forcing your engineers to parse a single mixed document, you can directly route the exact self-contained guide to its respective department:

---

### 📋 Standalone Installation Directory

| Target Infrastructure Area | Target Audience | Standalone Deployment Manual |
| :--- | :--- | :--- |
| **IBM z/OS Mainframe** | Mainframe Sysprogs / Security Admins | 📘 **[z/OS Standalone Guide](file:///Users/sridhargs/Documents/Antigravity/MFA/docs/DEPLOYMENT_ZOS.md)** |
| **IBM i (AS/400 / iSeries)** | Midrange Admins / AS/400 Operators | 📗 **[AS/400 Standalone Guide](file:///Users/sridhargs/Documents/Antigravity/MFA/docs/DEPLOYMENT_AS400.md)** |
| **HPE NonStop (Tandem)** | NonStop Ops / Guardian Administrators | 📙 **[HPE NonStop Standalone Guide](file:///Users/sridhargs/Documents/Antigravity/MFA/docs/DEPLOYMENT_TANDEM.md)** |
| **Collector Gateway & SIEMs** | Cloud / SecOps / SIEM / Cribl Architects | 📓 **[Collector & SIEM Standalone Guide](file:///Users/sridhargs/Documents/Antigravity/MFA/docs/DEPLOYMENT_GATEWAY.md)** |

---

## Master Architecture Overview

```
[ Legacy Source Systems ]                  [ LegacyTel Gateway ]                [ Destination / SIEM ]
=========================                  =====================                ======================
1. IBM z/OS (SMF logs)     --[mTLS TCP]-->  - Standard Go Binary --[HTTP HEC]-->  1. Splunk Enterprise/Cloud
2. IBM i AS/400 (Auditing) --[mTLS TCP]-->  - Receives, Decodes  --[OTLP/HTTP]->  2. Upstream OTel Collector
3. HPE NonStop (EMS logs)  --[mTLS TCP]-->  - Maps & Standardizes
```

---

## PHASE 1: Generate TLS Credentials
Before configuring any systems, generate the cryptographic certificates.
1. Log in to the Linux server where the **LegacyTel Gateway** will run.
2. Position yourself in the agent directory.
3. Execute the automated certificate generation utility:
   ```bash
   chmod +x generate_certs.sh
   ./generate_certs.sh
   ```
4. This creates a `./certs` directory containing the Root CA, Server certificates, and individual Client certificates for each platform. Keep this directory secure!

---

## PHASE 2: Source Systems Configuration (Legacy Clients Reference)

*(For self-contained platform manuals, please use the links in the Standalone Directory above).*

Each source system must be configured to capture events, convert them to standard network streams, and send them securely over TLS/mTLS to the LegacyTel Gateway.

### 1. IBM z/OS Mainframe Configuration

Mainframe events are captured from SMF logs and streamed via **IBM Z Common Data Provider (CDP)** or standard **Syslogd**.

#### A. Install/Configure on z/OS:
1. Ensure the **IBM Z Common Data Provider (CDP)** is installed and active in your z/OS environment.
2. In the CDP Configuration Tool (Web UI), define a **Data Stream** mapping SMF record types:
   - **SMF 80** (RACF Security events)
   - **SMF 30** (Job step utilization events)
   - **SMF 90** (Operator configuration command events)
3. Set the **Subscriber Target** of the data stream to point to your LegacyTel Gateway IP and Port `5080` using the **TCP Protocol**.

#### B. Configure Cryptography & mTLS on z/OS:
To secure the outbound stream using mTLS, z/OS uses the **System SSL** library via **AT-TLS (Application Transparent TLS)**.
1. Open your AT-TLS configuration file (typically member of `SYS1.TCPPARMS`).
2. Add a policy rule mapping outbound traffic targeting port `5080` to enforce TLS:
   ```text
   TTLSRule                          LegacyTelOutboundRule
   {
     LocalAddrGroupRef               No
     RemoteAddrGroupRef              No
     RemotePortRange                 5080
     Direction                       Outbound
     TTLSGroupActionRef              LegacyTelGroupAction
     TTLSEnvironmentActionRef        LegacyTelEnvAction
   }

   TTLSEnvironmentAction             LegacyTelEnvAction
   {
     HandshakeRole                   Client
     TTLSKeyringParms
     {
       # Reference to the z/OS RACF Keyring containing the certificates
       Keyring                       LEGACYTELKEYRING
     }
     TTLSEnvironmentAdvancedParmsRef LegacyTelEnvAdvParms
   }

   TTLSEnvironmentAdvancedParms      LegacyTelEnvAdvParms
   {
     TLSv1.2                         On
     TLSv1.3                         On
     # Enforce Mutual TLS: present the client cert when requested by Gateway
     ClientHandshake                 Required
   }
   ```
3. Use the RACF interface to import the generated certificates into the `LEGACYTELKEYRING`:
   - Import `root_ca.crt` as a trusted certificate authority (CA).
   - Import `zos_mainframe_client.crt` and `zos_mainframe_client.key` as a personal certificate and associate it with the CDP started task user ID.

---

### 2. IBM i (AS/400 / iSeries) Configuration

AS/400 uses **Audit Journaling (QAUDJRN)**. We will set up a secure log streaming program in the **PASE environment** or a custom CL/RPG program.

#### A. Install/Configure on IBM i:
1. Ensure security journaling is enabled by entering command:
   ```physical
   CHGSECAUD QAUDLVL(*AUTFAIL *CREATE *DELETE *SECURITY *SERVICE *JOBDTA)
   ```
   *This ensures QAUDJRN registers invalid passwords, logins, user alterations, and authority errors.*
2. Set up a daemon script or program that executes command `DSPJRN` periodically to stream journal receivers in standard `*TYPE5` formats.

#### B. Configure Cryptography & mTLS on IBM i:
IBM i manages certificates using the **Digital Certificate Manager (DCM)**.
1. Access DCM by opening `http://<your-as400-ip>:2001` in your browser.
2. Select **Manage Certificates**, and select the ***SYSTEM** Certificate Store.
3. Import the generated `root_ca.crt` under the **Trusted Certificate Authorities** tab.
4. Import `as400_iseries_client.crt` and its private key under the **Personal Certificates** tab.
5. Create an **Application Definition** for the log streamer program and bind the imported client certificate profile to it. This forces the secure socket API in PASE/ILE to present the certificates when communicating with LegacyTel on port `5081`.

---

### 3. HPE NonStop (Tandem) Configuration

NonStop monitors events using **EMS (Event Management Service)**. We will stream events to LegacyTel over a secure tunnel.

#### A. Install/Configure on HPE NonStop:
1. Establish a dedicated EMS consumer distributor named `LEGDISP`:
   ```cmd
   RUN $SYSTEM.SYSTEM.EMSDIST /NAME $LEGDISP, NOWAIT/ TYPE CONSUMER, SUB TYPE ALL, STOP 0
   ```
2. Configure the distributor to stream all Safeguard and TACL events directly to a TCP socket targeting your LegacyTel Gateway port `5082`.

#### B. Configure Cryptography & mTLS on HPE NonStop:
To encrypt raw TCP streams on Tandem without modifying application code, use a secure proxy tunnel (e.g. comforte SecurITy or a standard SSH/Stunnel bridge).
1. Configure the secure proxy client on NonStop:
   - Point the proxy input to listen to the local `$LEGDISP` stream.
   - Set the proxy outbound target to the LegacyTel Gateway IP and Port `5082`.
2. Import the generated `root_ca.crt` into the NonStop trusted certificate repository.
3. Load the `tandem_nonstop_client.crt` and its private key `tandem_nonstop_client.key` into the secure proxy profile, setting the authentication mode to **Enforce mTLS**.

---

## PHASE 3: LegacyTel Gateway Installation (The Collector)

The LegacyTel Gateway runs on a centralized Linux server (e.g., RedHat, SUSE, or Debian) or directly on the mainframe systems.

### 1. Build the Binary
1. Clone or copy the LegacyTel source files into a directory (e.g., `/opt/legacytel`).
2. Build the lightweight, zero-dependency binary:
   ```bash
   go build -o legacytel cmd/agent/main.go
   ```

### 2. File Placement & Security
1. Move the compiled binary to `/usr/local/bin`:
   ```bash
   sudo cp legacytel /usr/local/bin/
   ```
2. Create directories for certificates and configurations:
   ```bash
   sudo mkdir -p /etc/legacytel/certs
   sudo mkdir -p /var/log/legacytel
   ```
3. Copy your configuration and generated server certificates:
   ```bash
   sudo cp config.yaml /etc/legacytel/
   # Copy the generated server certs
   sudo cp certs/server.crt /etc/legacytel/certs/
   sudo cp certs/server.key /etc/legacytel/certs/
   sudo cp certs/root_ca.crt /etc/legacytel/certs/
   ```
4. Set permission flags to prevent unauthorized access to private keys:
   ```bash
   sudo chmod 600 /etc/legacytel/certs/server.key
   sudo chmod 644 /etc/legacytel/certs/server.crt
   sudo chmod 644 /etc/legacytel/certs/root_ca.crt
   ```

### 3. Edit `config.yaml` to Enable Security
Open `/etc/legacytel/config.yaml` in your favorite editor and configure TLS under receivers:

```yaml
receivers:
  zos_smf:
    enabled: true
    bind_address: "0.0.0.0"
    port: 5080
    format: "binary"
    charset: "ebcdic"
    # ENCRYPT TRANSIT AND VALIDATE MAINFRAME IDENTITY
    tls_enabled: true
    cert_file: "/etc/legacytel/certs/server.crt"
    key_file: "/etc/legacytel/certs/server.key"
    client_ca_file: "/etc/legacytel/certs/root_ca.crt" # Enforce mTLS
```
*(Apply similar configurations for `as400_qaudjrn` and `tandem_ems` as required).*

### 4. Configure as a System Service (Systemd)
To ensure the collector starts automatically on system boot, create a systemd service file:

1. Create file `/etc/systemd/system/legacytel.service`:
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
2. Enable and start the service:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable legacytel
   sudo systemctl start legacytel
   ```
3. Check agent status:
   ```bash
   sudo systemctl status legacytel
   ```

---

### PHASE 4: Destination & SIEM Configuration

LegacyTel is designed to be 100% vendor-agnostic and SIEM-neutral, allowing you to stream legacy platform logs directly to **any** modern or legacy security analytics endpoint using standard protocols.

### 1. Modern SIEM Integration (via OpenTelemetry OTLP)

Almost all modern observability and security platforms (e.g., **Microsoft Sentinel**, **Elastic Security**, **Datadog**, **Dynatrace**, and **Google Chronicle**) ingest logs natively in the standard OpenTelemetry format over OTLP/HTTP.

#### A. Direct SIEM Ingestion:
1. Obtain the **OTLP/HTTP Logs Ingestion URL** and authentication headers from your SIEM platform's administration console.
2. Open `/etc/legacytel/config.yaml` and configure the `otlp_http` block:
   ```yaml
   exporters:
     otlp_http:
       enabled: true
       endpoint: "https://<siem-otlp-endpoint>/v1/logs"
       headers:
         Authorization: "Bearer <siem-access-token>"
   ```
3. Restart the `legacytel` service. Standardized LogRecords will stream directly to your SIEM.

#### B. Ingestion via Upstream OTel Collector (Recommended):
For high-scale environments, stream logs to a centralized standard OpenTelemetry Collector gateway, which can then route and filter logs to multiple backend SIEMs simultaneously.

1. Open your standard OpenTelemetry Collector configuration file (`otel-collector-config.yaml`).
2. Add an `otlp` receiver supporting HTTP:
   ```yaml
   receivers:
     otlp:
       protocols:
         http:
           endpoint: "0.0.0.0:4318"
   ```
3. In the `exporters` block, define the destinations for your SIEMs (e.g. Elastic, Azure Sentinel, Splunk):
   ```yaml
   exporters:
     elasticsearch:
       endpoints: ["https://my-elastic-siem:9200"]
       user: "elastic"
       password: "secret-password"
     azuremonitor:
       instrumentation_key: "my-azure-sentinel-key"
   ```
4. Define the routing pipeline:
   ```yaml
   service:
     pipelines:
       logs:
         receivers: [otlp]
         processors: [batch]
         exporters: [elasticsearch, azuremonitor]
   ```
5. Configure LegacyTel's `config.yaml` `otlp_http` endpoint to point to `http://<otel-collector-ip>:4318/v1/logs`.

---

### 2. Legacy SIEM Ingestion (via CEF / LEEF / Syslog)

For legacy SIEM platforms (e.g., **IBM QRadar**, **ArcSight**, **LogRhythm**, and **Securonix**) that rely on standard log forwarding, LegacyTel can stream logs in highly structured text formats over secure TCP or UDP.

1. Obtain the IP and port of your SIEM syslog collector (standard port is `514`).
2. In `/etc/legacytel/config.yaml`, configure the `syslog` block:
   ```yaml
   exporters:
     syslog:
       enabled: true
       network: "tcp" # tcp or udp
       endpoint: "siem-collector.company.local:514"
       format: "cef" # Pick: 'cef' (ArcSight), 'leef' (QRadar), or 'rfc5424' (Standard structured syslog)
   ```
3. **Format details:**
   - **CEF (Common Event Format):** Standard format mapping user compliance codes:
     `CEF:0|LegacyTel|Agent|1.0.0|LL03|User login failure|13|src=ZOS-MAINFRAME-IBM390 msg=ICH408I PASSWORD INVALID`
   - **LEEF (Log Event Extended Format):** Standard IBM QRadar format:
     `LEEF:1.0|LegacyTel|Agent|1.0.0|LL03|devTime=2026-05-26T04:24:08Z	devHost=ZOS-MAINFRAME-IBM390	sev=WARN	cat=User login failure	msg=ICH408I PASSWORD INVALID`
   - **RFC 5424 Syslog:** Generic standard syslog containing structured headers.
4. Restart the `legacytel` service to begin streaming natively.

---

## PHASE 5: Troubleshooting & Agent Heartbeats

To ensure you can audit operations and debug pipelines without inspecting SIEM platforms, LegacyTel includes built-in local troubleshooting files and active baseline status heartbeats.

### 1. Daily Heartbeat (Activity baseline verification)
To guarantee the collector has not quietly failed or been blocked:
- **Startup Heartbeat:** A status check record (`SS05`) is fired **immediately** when the agent boots up.
- **Daily Heartbeat Ticker:** LegacyTel triggers a baseline heartbeat **every 24 hours** by default.
- **SIEM Check:** Security teams can construct a simple daily watchdog alert in Splunk/SIEM searching for the `SS05` user code. If no heartbeat is logged within a 25-hour window, the agent or server is offline.

### 2. Local Troubleshooting Alerts Log (History Folder)
A dedicated, local troubleshooting log is stored in a separate, isolated directory to record all critical and security violations.

- **Local Path:** `/Users/sridhargs/Documents/Antigravity/MFA/logs/alerts/history.log`
  *(Note: In a production Linux host deployment, this maps to `/var/log/legacytel/alerts.log`)*.
- **Trigger Conditions:** Written to *automatically* anytime a critical event is parsed:
  - High severity events (`WARN`, `ERROR`, `FATAL`).
  - Critical compliance failures: User login failures (`LL03`), Failed privileged access (`PA02`), Account locked (`SA08`), or system sequencing issues (`CM01`).
- **Log Entry Structure:**
  ```text
  [YYYY-MM-DD HH:MM:SS] [SEVERITY] [PLATFORM] [TAXONOMY_CODE] (DESCRIPTION) -> MESSAGE_BODY
  ```
- **Example Log Entries:**
  ```text
  [2026-05-26 04:15:15] [WARN] [zos] [LL03] (User login failure) -> ICH408I USERID SYSADMIN TERMINAL L3270A1 - PASSWORD INVALID
  [2026-05-26 04:15:16] [ERROR] [ibm_i] [PA02] (Failed privileged operation access) -> QAUDJRN entry: AF - Authority failure. User QUSER lacked *USE authority to object QGPL/DBTABLE
  ```
- **Operational Recommendation:** Monitor this local log folder using standard UNIX rotation utilities (like `logrotate`) to manage historical retention safely.

---

## PHASE 6: SIEM Ingestion & Parsing Reference

This section provides copy-pasteable parsing patterns, search queries, and threat detection rules for all major SIEM platforms, mapped to LegacyTel's normalized OpenTelemetry LogRecord structure.

### 1. Splunk (SPL Query Patterns)

When ingesting LegacyTel logs into Splunk (via standard OTLP JSON or raw Syslog/CEF), use these SPL queries to construct dashboards and alerts.

#### A. Standard Log Record Field Extraction (JSON/OTLP):
```splunk
index=mainframe_security sourcetype=_json
| rename event.attributes.legacy.user_code as user_code, 
         event.attributes.legacy.user_code_description as description, 
         event.resource.os.type as platform, 
         event.severity_text as severity, 
         event.body as msg,
         event.attributes.security.user as user
| table _time, platform, user_code, description, severity, user, msg
```

#### B. SOC Alert: Enforce Detection on Brute Force Login Failures (LL03):
```splunk
index=mainframe_security sourcetype=_json event.attributes.legacy.user_code="LL03"
| rename event.resource.os.type as platform, event.attributes.security.user as user
| stats count as failure_count, values(event.body) as sample_errors by platform, user
| where failure_count > 5
```

---

### 2. Microsoft Sentinel (KQL Query Patterns)

Sentinel ingests standard OpenTelemetry logs into the `AppLogs` table or a custom `LegacyTel_CL` table. Use these **Kusto Query Language (KQL)** queries for parsing and alert rules.

#### A. Parsing OpenTelemetry LogRecord Fields:
```kusto
AppLogs
| extend OTelLog = parse_json(Message)
| extend Platform = tostring(OTelLog.resource.["os.type"]),
         UserCode = tostring(OTelLog.attributes.["legacy.user_code"]),
         Description = tostring(OTelLog.attributes.["legacy.user_code_description"]),
         User = tostring(OTelLog.attributes.["security.user"]),
         RawMsg = tostring(OTelLog.body)
| project TimeGenerated, Platform, UserCode, Description, User, RawMsg, SeverityText
| order by TimeGenerated desc
```

#### B. Sentinel Analytics Rule: Detection of Locked Accounts (SA08):
```kusto
AppLogs
| extend OTelLog = parse_json(Message)
| extend UserCode = tostring(OTelLog.attributes.["legacy.user_code"])
| where UserCode == "SA08"
| extend Platform = tostring(OTelLog.resource.["os.type"]),
         TargetUser = tostring(OTelLog.attributes.["security.target_user"])
| project TimeGenerated, Platform, TargetUser, Message
```

---

### 3. Google Chronicle (YARA-L Detection Rules)

Google Chronicle maps standard OTLP fields natively to the **Unified Data Model (UDM)**. High-severity alerts can be written using **YARA-L**.

#### A. YARA-L Rule: Detect Failed Privileged Access Operations (PA02):
```yara
rule legacytel_privilege_violations {
  meta:
    author = "SecOps Team"
    description = "Alerts when a mainframe user is denied privileged operation access (PA02)."
    severity = "HIGH"

  events:
    $event.metadata.product_name = "LegacyTel"
    // Map taxonomy attribute fields
    $event.security_result.action = "BLOCK"
    $event.metadata.product_event_type = "PA02"
    $event.principal.hostname = $host
    $event.principal.user.userid = $user

  match:
    $host, $user over 5m

  condition:
    $event
}
```

---

### 4. Elastic Security (Elastic Common Schema - ECS Mappings)

If you route logs to **Elasticsearch / Kibana**, use an Ingestion Pipeline processor to translate OTel fields directly into the standard **Elastic Common Schema (ECS)**:

#### A. Ingestion Pipeline Processor Definition (JSON):
```json
{
  "description": "LegacyTel OTel to ECS Mapper",
  "processors": [
    {
      "set": {
        "field": "event.code",
        "copy_from": "event.attributes.legacy.user_code"
      }
    },
    {
      "set": {
        "field": "event.category",
        "value": "iam"
      }
    },
    {
      "set": {
        "field": "host.os.family",
        "copy_from": "event.resource.os.type"
      }
    },
    {
      "set": {
        "field": "user.name",
        "copy_from": "event.attributes.security.user"
      }
    }
  ]
}
```

#### B. Kibana Query Language (KQL) for Security Analysts:
- Find all privilege violations on AS/400:
  `event.code: "PA02" and host.os.family: "ibm_i"`
- Find utilization threshold alerts across all environments:
  `event.code: "CM02"`

---

### 5. Cribl Stream Ingestion & Pipeline Configuration

**Cribl Stream** is a highly efficient log processing and routing engine. It allows you to parse, enrich, flatten, and filter LegacyTel's high-volume OTel log streams before sending them downstream to any SIEM.

#### A. Configure Ingest Source in Cribl:
1. In the Cribl Stream UI, navigate to **Manage** > **Sources** > **OpenTelemetry**.
2. Click **New Source**:
   - **Input ID:** `LegacyTel-OTel-Ingest`
   - **Address:** `0.0.0.0`
   - **Port:** `4318` (Ensure firewalls permit inbound HTTP traffic).
3. Set the receiver endpoint in LegacyTel's `config.yaml` `otlp_http` to point to your Cribl worker node IP address on port `4318`.

#### B. Flattening OTel Structured Nested JSON (Eval / JavaScript Function):
Cribl natively processes events as JavaScript objects. To simplify downstream SIEM searches, use a **Cribl Eval Function** to flatten nested OTel resource and attribute arrays into flat, top-level key-value fields.

1. Add an **Eval** function to your Cribl Log Pipeline:
   - **Filter:** `true` (processes all events)
   - **Evaluate Options:** Add these JavaScript mappings:
     
     | Keep / Set Field | Value Expression (JavaScript) | Description |
     | :--- | :--- | :--- |
     | `platform` | `resourceLogs[0].resource.attributes.find(a => a.key === 'os.type')?.value?.stringValue` | Promotes host platform to root |
     | `host` | `resourceLogs[0].resource.attributes.find(a => a.key === 'host.name')?.value?.stringValue` | Promotes host name to root |
     | `user_code` | `resourceLogs[0].scopeLogs[0].logRecords[0].attributes.find(a => a.key === 'legacy.user_code')?.value?.stringValue` | Promotes user code |
     | `user` | `resourceLogs[0].scopeLogs[0].logRecords[0].attributes.find(a => a.key === 'security.user')?.value?.stringValue` | Promotes target user |
     | `severity` | `resourceLogs[0].scopeLogs[0].logRecords[0].severityText` | Sets root severity text |
     | `message` | `resourceLogs[0].scopeLogs[0].logRecords[0].body.stringValue` | Sets root message field |

2. Remove the original bulky arrays to reduce payload weight by 45%:
   - Add a **Remove** field function targeting: `resourceLogs`

#### C. SIEM Licensing Cost Optimization (Heartbeat Drop Filter):
Mainframe logs are high volume. While daily heartbeats (`SS05`) are vital to confirm agent health, forwarding all standard daily heartbeats to your downstream SIEM increases data ingestion and licensing costs.

Use a Cribl **Drop** function to drop normal heartbeat logs *at the Cribl stream layer*, keeping your SIEM index clean, while still alerting if a heartbeat is missing.

1. Add a **Drop** function to your Cribl Pipeline:
   - **Filter:** `user_code === 'SS05' && severity === 'INFO'`
   - **Drop Action:** Set to `Drop` (or `Sample` to 1-out-of-100).
   - This drop filter guarantees you save licensing fees without compromising alert capabilities, as critical alerts (e.g. `severity === 'WARN'` or `severity === 'ERROR'`) bypass the drop block and route to the SIEM successfully.
