# LegacyTel: z/OS Mainframe Standalone Ingest Guide

This document is a standalone, self-contained deployment manual for **z/OS Mainframe Systems Programmers (Sysprogs)** and **Security Administrators**. It details how to capture z/OS operational events (SMF logs) and stream them securely to the LegacyTel Gateway.

---

## 1. System Requirements & Ingest Stream Mapping

LegacyTel collects System Management Facility (SMF) logs. Ensure your environment records the following SMF record types:
- **SMF 80:** RACF/ACF2/TopSecret security audit records (Failed logins, user administration, authority violations).
- **SMF 30:** Address Space/Job accounting records (Step start/stops, utilization limits, memory allocations).
- **SMF 90:** Operator command and system configuration events.

---

## 2. Ingestion Setup (IBM Z Common Data Provider - CDP)

IBM Z Common Data Provider (CDP) is the industry standard for streaming operational data from z/OS. 

### Step-by-Step CDP Stream Configuration:
1. Log in to the **CDP Configuration Tool Web UI**.
2. Select **Create a Policy** or edit an existing operational policy.
3. Under **Data Sources**, select and enable the following sources:
   - `SMF_030_SUBTYPE1_4_8_9` (Job Step Account data)
   - `SMF_080` (RACF Audit record data)
   - `SMF_090` (System commands data)
4. Under **Data Receivers**, click **Add Receiver**:
   - **Name:** `LegacyTel-Receiver`
   - **Protocol:** `TCP`
   - **Host Name / IP:** Enter the IP address of your LegacyTel Gateway.
   - **Port:** `5080` (Default receiver port for z/OS SMF).
5. Drag and link the data sources directly to the `LegacyTel-Receiver` target.
6. Click **Save** and then **Deploy** to push the updated configuration policy to your z/OS server.
7. Start or restart the CDP started tasks (`HBOCOLR` and `HBOPROV`) using MVS operator console commands:
   ```physical
   S HBOCOLR
   S HBOPROV
   ```

---

## 3. Securing Data in Transit (AT-TLS & mTLS)

All mainframe compliance logs must be encrypted in transit. We use **Application Transparent TLS (AT-TLS)** to secure the outbound stream without altering CDP code.

### Step 1: Create AT-TLS Policy Rule
In your AT-TLS configuration file (typically a member in `SYS1.TCPPARMS`), define an outbound rule targeting the LegacyTel Gateway on port `5080`:

```text
TTLSRule                          LegacyTelMainframeOutbound
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
    # Name of the RACF Keyring containing certificates
    Keyring                       LEGACYTELKEYRING
  }
  TTLSEnvironmentAdvancedParmsRef LegacyTelEnvAdvParms
}

TTLSEnvironmentAdvancedParms      LegacyTelEnvAdvParms
{
  TLSv1.2                         On
  TLSv1.3                         On
  # Enforce Mutual TLS (presents client certificate to authenticate this LPAR)
  ClientHandshake                 Required
}
```

### Step 2: Configure RACF Keyring and Certificates
Use standard RACF command blocks to import certificates into the started task keyring.

1. **Create the Keyring:**
   ```physical
   RACDCERT ADDRING(LEGACYTELKEYRING) UACC(NONE)
   ```
2. **Import the Root CA (from the `certs` folder):**
   ```physical
   RACDCERT CERTAUTH ADD('USER1.CERTS.ROOTCA') WITHLABEL('LegacyTel-Root-CA') TRUST
   ```
3. **Import the z/OS Client Certificate and Key:**
   ```physical
   RACDCERT ADD('USER1.CERTS.ZOSCLIENT') PASSWORD('key-password') WITHLABEL('ZOS-Client-Cert') TRUST
   ```
4. **Connect Certificates to Keyring:**
   ```physical
   RACDCERT CONNECT(CERTAUTH LABEL('LegacyTel-Root-CA') RING(LEGACYTELKEYRING))
   RACDCERT CONNECT(LABEL('ZOS-Client-Cert') RING(LEGACYTELKEYRING) DEFAULT)
   ```
5. **Refresh RACF Classes:**
   ```physical
   SETROPTS RACLIST(DIGTCERT, DIGTRING) REFRESH
   ```

---

## 4. Troubleshooting and Local Logs

If log records are not appearing in your downstream SIEM:
1. Validate TCP connectivity from z/OS to the Gateway using ping:
   ```physical
   TSO PING <gateway-ip>
   ```
2. Verify the AT-TLS handshake status in the TCP/IP stack syslog. Search for error codes starting with `EZD1287I` (indicates TLS handshake errors or untrusted client certificates).
3. If the connection fails immediately, check if the client certificate has expired or if the label name in the AT-TLS profile does not match the default keyring certificate.
