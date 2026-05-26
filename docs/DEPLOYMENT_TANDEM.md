# LegacyTel: HPE NonStop (Tandem) Standalone Ingest Guide

This document is a standalone, self-contained deployment manual for **HPE NonStop (Tandem) Systems Administrators** and **Operator Supervisors**. It details how to capture system-level Event Management Service (EMS) logs and stream them securely to the LegacyTel Gateway.

---

## 1. System Requirements & Ingest Stream Mapping

LegacyTel collects operational and audit events from the **Event Management Service (EMS)**. Ensure your environment maps the following critical subsystems:

- **TACL (Tandem Advanced Command Language):** Triggers on operator logons (`LL01`), logoffs (`LL02`), and authentication errors (`LL03`).
- **SAFE (Safeguard Security Product):** Triggers on password resets (`SA07`), account lockouts (`SA08`), and authority violations (`PA02`).
- **TMF (Transaction Monitoring Facility):** Triggers on system start/stop (`SS01`), transaction dumps (`SS03`), and sequencing failures (`CM01`).
- **PATHWAY (Application Server Manager):** Triggers on application class starts (`SS01`) and utilization limits (`CM02`).

---

## 2. Ingestion Setup (Enabling EMS Logging)

Tandem events are collected using a dedicated EMS consumer distributor that processes and redirects events over TCP.

### Step-by-Step Configuration:
1. Log in to the Guardian command prompt (**TACL**) using a privileged profile (e.g. `255,255` or security officer).
2. Create and start a persistent EMS Consumer Distributor named `LEGDISP` in the background:
   ```cmd
   RUN $SYSTEM.SYSTEM.EMSDIST /NAME $LEGDISP, NOWAIT/ TYPE CONSUMER, SUB TYPE ALL, STOP 0
   ```
3. Configure `LEGDISP` to route events to a TCP socket targeting your LegacyTel Gateway IP and Port `5082`.
4. Verify the distributor is running actively:
   ```cmd
   STATUS $LEGDISP
   ```

---

## 3. Securing Data in Transit (mTLS Proxy Setup)

HPE NonStop streams are encrypted using a secure proxy wrapper (e.g. **comforte SecurITy**, **PROGINET**, or a standard SSH/Stunnel tunnel bridge) to ensure complete confidentiality and enforce mTLS.

### Step 1: Install Gateway CA and Client Keys
1. Copy the generated certs from the LegacyTel `certs` folder onto your NonStop OSS filesystem:
   - `root_ca.crt` (Root CA file)
   - `tandem_nonstop_client.crt` (Client Certificate)
   - `tandem_nonstop_client.key` (Client Private Key)
2. Place these files in a secure directory (e.g. `/usr/local/legacytel/certs/`) and restrict file access permissions:
   ```bash
   chmod 600 /usr/local/legacytel/certs/tandem_nonstop_client.key
   chmod 644 /usr/local/legacytel/certs/tandem_nonstop_client.crt
   ```

### Step 2: Configure the Secure Tunnel Wrapper
Establish a local tunnel configuration profile (e.g. `/etc/stunnel/tandem_stunnel.conf`):

```text
client = yes
pid = /var/run/tandem_stunnel.pid

[ems-log-stream]
# Local port that receives the plain EMSDIST socket
accept = 127.0.0.1:9082
# Outbound target targeting the LegacyTel Gateway port 5082
connect = <legacytel-gateway-ip>:5082
# Load client credentials for mTLS validation
cert = /usr/local/legacytel/certs/tandem_nonstop_client.crt
key = /usr/local/legacytel/certs/tandem_nonstop_client.key
CAfile = /usr/local/legacytel/certs/root_ca.crt
# Enforce strict server verification
verifyPeer = yes
```

### Step 3: Redirect EMSDIST
Modify your EMS Consumer Distributor `LEGDISP` config to direct its output socket to `127.0.0.1:9082` (the input of the secure tunnel). The tunnel automatically wraps the stream in a TLS layer and presents the client credentials during the mTLS handshake with port `5082` on the Gateway.

---

## 4. Troubleshooting and Local Logs

If log records are not appearing in your downstream SIEM:
1. Ping the Gateway server from the TACL prompt:
   ```cmd
   SCF PINGRM RMTSYS <gateway-ip>
   ```
2. Verify the EMS Distributor status using:
   ```cmd
   STATS $LEGDISP
   ```
3. Inspect active tunnel process console logs (e.g. in OSS syslog or Stunnel debug output) for certificate handshake rejections (`verify failed` or `connection reset by peer`).
