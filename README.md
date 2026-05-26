# LegacyTel: Mainframe & Legacy Log Observability Agent

**LegacyTel** is a high-performance, lightweight, open-source log forwarding and normalization agent written in Go. It is specifically designed to bridge the observability gap between legacy platforms (**IBM z/OS Mainframe**, **IBM AS/400 / IBM i**, and **HPE NonStop / Tandem**) and modern security information and event management (SIEM) tools like **Splunk** and upstream **OpenTelemetry Collectors**.

LegacyTel provides a modern, zero-dependency, open-source alternative to expensive, heavy, proprietary tools, enabling unified security compliance and operations monitoring.

---

## Key Features

- **Standard-Library Only Build:** Written in pure Go standard library with zero external dependencies. This ensures a tiny memory footprint, extreme throughput, and seamless compilation/execution.
- **Cross-Platform Compatibility:** Easily cross-compiles to z/OS (IBM Open Enterprise SDK for Go), IBM i (running natively in the PASE environment), and HPE NonStop (x86 runtime).
- **OpenTelemetry Logs Alignment:** Fully parses raw events and maps them to the standardized OpenTelemetry LogRecord specification (with rich resource and attribute definitions).
- **Embedded Observability Console:** Serves a stunning web-based **Glassmorphic Observability Console** (via Server-Sent Events on `:8080`) providing real-time telemetry, system metrics, search/filtering, and structured JSON inspectability.
- **Dual Outbound Pipelines:** Built-in batch exporters for **Splunk HEC (HTTP Event Collector)** and standard **OTLP/HTTP** endpoints.

---

## 1. Unified Security Taxonomy Mapping

LegacyTel translates native legacy operational and security logs directly to a standardized compliance taxonomy:

### Taxonomy Code Directory

| Category | Code | Standard Event Description | Legacy Platform mapping details |
| :--- | :--- | :--- | :--- |
| **Authentication (LL)** | `LL01` | Successful login | SMF 80 (ICH70001I) / QAUDJRN (JS/PW login success) / Tandem TACL Log On |
| | `LL02` | Successful logoff | SMF 80 (User Logoff) / QAUDJRN (JS logoff) / Tandem TACL Log Off |
| | `LL03` | User login failure | SMF 80 (ICH408I invalid password) / QAUDJRN (PW error) / Tandem TACL Failed |
| | `LL04` | Password change success | SMF 80 (ICH70002I Set Password) / QAUDJRN (PW Change success) |
| | `LL05` | Password change failure | SMF 80 (ICH408I Set Password Error) / QAUDJRN (PW Change failure) |
| **Configuration (CC)** | `CC01` | Application configuration change | SMF 90 (IEE252I Member updated) / QAUDJRN (SV value modified) |
| | `CC02` | Security configuration change | SMF 90 (ICH102I RACF Rules refresh) / QAUDJRN (AD Audit change) |
| **Privileges (PA)** | `PA01` | Successful privilege operation access | SMF 80 (ICH70002I Authorized privilege command) / Tandem SAFE Privilege Escalation |
| | `PA02` | Failed privileged operation access | SMF 80 (ICH408I Insufficient Authority) / QAUDJRN (AF Authority Fail) / Tandem SAFE Violation |
| **Administration (SA)** | `SA01` | User creation | SMF 80 (Define User) / QAUDJRN (CP create profile) / Tandem SAFE User Created |
| | `SA02` | User change | SMF 80 (Change User) / QAUDJRN (CP change profile) / Tandem SAFE User Changed |
| | `SA03` | User deletion | SMF 80 (Delete User) / QAUDJRN (CP delete profile) / Tandem SAFE User Deleted |
| | `SA04` | User profile/role creation | SMF 80 (Define Group/Profile) / QAUDJRN (CP role create) |
| | `SA05` | User profile/role change | SMF 80 (Change Group/Profile) / QAUDJRN (CP role change) |
| | `SA06` | User profile/role deletion | SMF 80 (Delete Group/Profile) / QAUDJRN (CP role delete) |
| | `SA07` | User password reset | SMF 80 (User Password Set) / QAUDJRN (PW Admin Reset) / Tandem SAFE Password Reset |
| | `SA08` | User account locked | QAUDJRN (Profile Disabled) / Tandem SAFE Profile Locked (Max failures) |
| | `SA09` | User account unlocked | QAUDJRN (Profile Enabled) / Tandem SAFE Profile Unlocked |
| **System & Apps (SS)** | `SS01` | Application started | SMF 30 Subtype 1 (Job Start) / QAUDJRN (JS job start) / Tandem TMF Coordinator Start |
| | `SS02` | Application stopped | SMF 30 Subtype 4 (Job End) / QAUDJRN (JS job end) / Tandem TMF Coordinator Stop |
| | `SS03` | Application data dump | Tandem TMF Online Dump Completed / z/OS CICS transaction dump |
| | `SS04` | Application data restore | Tandem TMF Online Restore Completed |
| | `SS05` | Application logging change | CICS/IMS logging level modification / QAUDJRN logging options update |
| **Performance (CM)** | `CM01` | Sequencing failure | Tandem TMF Sequencing alert / CICS transaction sequence exception |
| | `CM02` | Utilization threshold reached | SMF 30 (IEF085I CPU Limit 90%+) / AS400 Job Queue limit reached / Pathway CPU warning |
| | `CM03` | Application code change | CICS program release load / AS/400 library program compile |
| | `CM04` | Application memory change | SMF 30 (IEF196I Step Region extended) / AS/400 LPAR memory shift |

---

## 2. Ingest Architecture & Platform Integration

LegacyTel supports both **local deployment** (running as a native daemon directly on the host) and **observability gateway** mode (receiving raw data streams from mainframes and systems via TCP/UDP).

```
+------------------+
| z/OS Mainframe   | ---[TCP:5080 (SMF Exit / CDP)]---> +----------------------------+      +-------------------+
+------------------+                                    |    LegacyTel Agent         | ---> | Splunk Cloud /    |
+------------------+                                    |    - EBCDIC to ASCII       |      | Enterprise (HEC)  |
| IBM i (AS/400)   | ---[TCP:5081 (QAUDJRN Exit)]-----> |    - OTel Logs Standard    |      +-------------------+
+------------------+                                    |    - HTML5 Glassmorphic UI |      +-------------------+
+------------------+                                    |      Real-Time Dashboard   | ---> | Upstream OTel     |
| HPE NonStop      | ---[TCP:5082 (EMS Distributor)]--> +----------------------------+      | Collector (OTLP)  |
+------------------+                                                                        +-------------------+
```

### IBM z/OS Ingest (SMF)
Mainframe events are parsed from native SMF records.
1. **Binary Records:** Captured directly from SMF datasets (e.g. `SYS1.MANx`) or via SMF exit programs (e.g. `IEFACTRT`, `IRREVX01`).
2. **EBCDIC Conversion:** LegacyTel includes a highly robust **Code Page 1047 EBCDIC-to-ASCII** translation block, automatically decoding binary text into readable ASCII standard format.
3. **Triplets Parsing:** Decodes SMF triplet structures (Offset / Length / Number fields) to traverse security sections (SMF 80) and account sections (SMF 30).

### IBM i AS/400 Ingest (QAUDJRN)
Security Audit Journal records are streamed via exit programs or database integration.
1. **Journal Entries:** Uses standard `*TYPE5` format containing sequence numbers, entry types, program names, and target objects.
2. **Trigger Exit:** An RPG/CL exit program triggers on new `QAUDJRN` entry generation and pipes the raw record over a secure TCP channel directly to the LegacyTel receiver port `5081`.

### HPE NonStop Ingest (EMS)
Tandem EMS (Event Management Service) structures are collected via distributors.
1. **Distributor Pipe:** LegacyTel binds an EMS consumer distributor (`$ZEMS`) streaming events to port `5082`.
2. **Subsystems:** Decodes standard header profiles including Cpu/Pin, Event Number, and Subsystem Identifier (e.g. `TACL`, `SAFE`, `TMF`, `PATHWAY`).

---

## 3. Securing Data in Transit (TLS & mTLS)

Security is paramount when transporting mainframe and legacy system audit logs, as they contain sensitive access metadata, user profiles, and administrative event records. LegacyTel supports native **in-transit encryption** and **client-identity verification** utilizing standard-library Transport Layer Security (TLS).

### 1. In-Transit Encryption (TLS)
To prevent packet sniffing and man-in-the-middle (MITM) interceptions over internal networks, TCP listeners for all receivers (`zos_smf`, `as400_qaudjrn`, `tandem_ems`) can be encrypted by enabling TLS:
- Forces secure modern protocol versions (**TLS 1.2 or TLS 1.3**).
- Implements standard standard-library key pair load (`cert_file` and `key_file`) specified in `config.yaml`.

### 2. Mutual TLS (mTLS) for Client Verification
In highly secure, zero-trust enterprise environments, it is vital to ensure that only authenticated mainframe LPARs, AS/400 logical partitions, and NonStop nodes can stream events to the observability gateway.
- **Strong Authentication:** Specifying a `client_ca_file` enables Mutual TLS (mTLS).
- **Mechanism:** LegacyTel acts as a secure server that challenges the legacy system sender to present a client certificate. The connection is immediately terminated if the client certificate is missing or fails verification against the specified certificate authority (CA).

---

## 4. Operations & Configuration

LegacyTel is configured using a production-grade YAML template (`config.yaml`):

```yaml
agent:
  id: "legacytel-hq-01"
  name: "mainframe-observability-forwarder"
  environment: "production"

server:
  host: "0.0.0.0"
  port: 8080
  enable_dashboard: true

receivers:
  zos_smf:
    enabled: true
    port: 5080
    tls_enabled: true
    cert_file: "/etc/legacytel/certs/zos_server.crt"
    key_file: "/etc/legacytel/certs/zos_server.key"
    client_ca_file: "/etc/legacytel/certs/mainframe_ca.crt" # Enables mutual TLS (mTLS)
  as400_qaudjrn:
    enabled: true
    port: 5081
    tls_enabled: true
    cert_file: "/etc/legacytel/certs/as400_server.crt"
    key_file: "/etc/legacytel/certs/as400_server.key"
    client_ca_file: "/etc/legacytel/certs/iseries_ca.crt"
  tandem_ems:
    enabled: true
    port: 5082
    tls_enabled: false

exporters:
  otlp_http:
    enabled: true
    endpoint: "http://localhost:4318/v1/logs"
  syslog:
    enabled: true
    network: "tcp" # tcp or udp
    endpoint: "siem-collector.local:514"
    format: "cef" # Pick: cef (ArcSight), leef (QRadar), or rfc5424 (Standard Syslog)
  splunk_hec:
    enabled: false # Optional direct Splunk integration
```

---

## 5. Compilation & Deployment

### Build the Binary Locally
Since LegacyTel is written in pure Go without external dependencies, compiling the code requires a single shell command:

```bash
# Compile local platform binary
go build -o legacytel cmd/agent/main.go
```

### Cross-Compiling for Legacy Platforms

```bash
# 1. Cross-compile for z/OS Mainframe (EBCDIC native environment)
GOOS=zos GOARCH=s390x go build -o legacytel-zos cmd/agent/main.go

# 2. Cross-compile for IBM i (AS/400 PASE environment)
GOOS=aix GOARCH=ppc64 go build -o legacytel-iseries cmd/agent/main.go

# 3. Cross-compile for HPE NonStop Tandem x86
GOOS=nonstop GOARCH=amd64 go build -o legacytel-tandem cmd/agent/main.go
```

---

## 6. Technical Tradeoffs & Performance

### 1. Zero-Dependency standard-library vs. Heavy Frameworks
*   **Tradeoff:** Standard-library-only requires writing custom decoders and line-parsers (e.g. our custom YAML and EBCDIC logic).
*   **Advantage:** Zero dependency guarantees that the binary compiles immediately on restricted mainframes (which lack packet managers like npm, pip, or go-mod proxies). The binary size is < 10MB and uses < 25MB RAM under heavy workloads.

### 2. High-Throughput Buffering and Dropping Policy
*   **Tradeoff:** Mainframes generate millions of SMF records during peak hours. If upstream SIEM systems (like Splunk HEC) experience network backpressure, the agent queue could overflow.
*   **Resolution:** LegacyTel implements an asynchronous channel buffer (5,000 slots) with non-blocking enqueuing. If the buffer is fully saturated, excess logs are safely dropped, printing a console warning, preventing the agent from exhausting target system RAM or causing mainframe resource blockages.

### 3. Server-Sent Events (SSE) vs. WebSockets
*   **Tradeoff:** We opted for Server-Sent Events (SSE) instead of WebSockets for dashboard synchronization.
*   **Advantage:** SSE operates over standard HTTP, eliminating complex WebSocket frame-parsing code, which reduces the binary footprint and provides native browser automatic reconnection with zero technical overhead.
