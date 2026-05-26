# LegacyTel: IBM i (AS/400) Midrange Standalone Ingest Guide

This document is a standalone, self-contained deployment manual for **IBM i (AS/400) Midrange Administrators** and **Security Operations**. It details how to capture system-level operational audits (QAUDJRN) and stream them securely to the LegacyTel Gateway.

---

## 1. System Requirements & Ingest Stream Mapping

LegacyTel collects security event logs from the **Audit Journal (QAUDJRN)**. Ensure your environment has auditing activated:

- **PW (Password Errors):** Triggers on invalid password logins (`LL03`), password changes (`LL05`), and password resets (`SA07`).
- **AF (Authority Failures):** Triggers on failed privileged operations (`PA02`).
- **CP (Profile Changes):** Triggers on user creation (`SA01`), user change (`SA02`), and user deletion (`SA03`).
- **SV (System Value changes):** Triggers on security configuration alterations (`CC02`).
- **JS (Job Session data):** Triggers on job step startup (`SS01`) and shutdowns (`SS02`).

---

## 2. Ingestion Setup (Enabling Audit Logging)

Audit journaling must be active on your IBM i partition to register events.

### Step-by-Step Configuration:
1. Log in to the IBM i green screen command line (`5250 Session`) using an administrator profile (e.g. `QSECOFR`).
2. Establish the audit journal receiver and the QAUDJRN journal if they do not exist:
   ```physical
   CHGSECAUD QAUDLVL(*AUTFAIL *CREATE *DELETE *SECURITY *SERVICE *JOBDTA)
   ```
3. Verify that the journal status is active by issuing command:
   ```physical
   WRKJRN JRN(QSYS/QAUDJRN)
   ```
4. Configure a background daemon script (running inside the **PASE environment** or a custom CL/RPG program) that reads journal receivers in standard `*TYPE5` format:
   ```physical
   DSPJRN JRN(QSYS/QAUDJRN) OUTPUT(*OUTFILE) OUTFILE(QGPL/AUDITOUT) OUTFILFMT(*TYPE5)
   ```
5. Configure the program to stream new table entries over a secure TCP socket targeting your LegacyTel Gateway IP and Port `5081`.

---

## 3. Securing Data in Transit (Digital Certificate Manager - DCM)

To secure outbound streams using Mutual TLS (mTLS), import certificates into the IBM i **Digital Certificate Manager (DCM)**.

### Step 1: Open DCM in your Web Browser
1. Ensure the administrative HTTP server is started on IBM i:
   ```physical
   STRTCPSVR SERVER(*HTTP) HTTPSVR(*ADMIN)
   ```
2. Navigate to `http://<your-iseries-ip>:2001` in your web browser.
3. Click on the **Digital Certificate Manager** link in the navigation panel.

### Step 2: Import Root CA and Client Certificates
1. Click **Select a Certificate Store** and choose the ***SYSTEM** store. (Enter the system certificate store password when prompted).
2. Go to **Manage Certificates** > **Import Certificate**.
3. Select **Trusted Certificate Authority (CA)**:
   - Click **Browse** and upload the generated `root_ca.crt` file.
   - Assign the label: `LegacyTel-Root-CA` and click **OK**.
4. Select **User or Client Certificate**:
   - Click **Browse** and upload the generated client bundle `as400_iseries_client.crt`.
   - Provide the private key password and assign the label: `AS400-Client-Cert`.

### Step 3: Map Application Profile
1. Go to **Manage Applications** > **Define Application**.
2. Create a new Client Application Definition named `LEGACYTEL_STREAMER`.
3. Set the Certificate Assignment to the imported `AS400-Client-Cert`. This forces the PASE/ILE socket API to present these TLS credentials when communicating with LegacyTel on port `5081`.

---

## 4. Troubleshooting and Local Logs

If log records are not appearing in your downstream SIEM:
1. Ping the Gateway server from the IBM i command line:
   ```physical
   PING RMTSYS('<gateway-ip>')
   ```
2. Inspect active jobs inside your logging subsystem (e.g. `QINTER` or `QSYSWRK`):
   ```physical
   WRKACTJOB SBS(QSYSWRK)
   ```
3. Look at job logs for your custom audit stream program using command `DSPJOBLOG` to check for TLS socket exceptions or handshake authentication failures.
