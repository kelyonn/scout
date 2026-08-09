# Runbook: Security incident

**Severity:** SEV1 · **Alert:** Auth token used from an unrecognised device,
registration, request to a prohibited source, outbound request to a private IP,
or any suspected compromise

**Containment before investigation. Always.** A compromised system doing more
work is a compromised system doing more damage.

---

## Step 1 — Contain (first 5 minutes)

```bash
ssh scout@<host> && cd /opt/scout

# Revoke every session immediately
docker compose exec postgres psql -U scout -c "
  UPDATE session SET revoked_at = now() WHERE revoked_at IS NULL;"

# Stop outbound activity and notifications
docker compose stop collector notifier

# Stop paid model calls
curl -X POST localhost:8080/api/v1/admin/llm/kill-switch -H "Cookie: $SESSION"
```

**If compromise is confirmed rather than suspected**, take the whole thing
offline. Availability is worth less than containment:

```bash
docker compose down
```

---

## Step 2 — Preserve evidence (before remediating)

Remediation destroys evidence. Snapshot first.

```bash
mkdir -p /root/incident-$(date +%Y%m%d-%H%M)
cd /root/incident-*

docker compose logs --no-color --since 72h > docker-logs.txt
journalctl --since "72 hours ago" --no-pager > system-logs.txt
cp /var/log/auth.log* .
docker compose exec -T postgres pg_dump -U scout \
  -t session -t webauthn_credential -t notification_channel > auth-tables.sql
ss -tunap > connections.txt
docker ps -a > containers.txt
crontab -l > crontab.txt 2>/dev/null
```

Take a Hetzner snapshot of the volume too — it is the most complete evidence and
it takes one click.

---

## Step 3 — Assess

### Was there unauthorized access?

```sql
-- Sessions from unexpected locations
SELECT id, ip_prefix, user_agent, created_at, last_seen_at
FROM session ORDER BY created_at DESC LIMIT 30;

-- Credentials you did not create
SELECT id, device_label, created_at, last_used_at, sign_count
FROM webauthn_credential ORDER BY created_at DESC;

-- Notification channels you did not add — a common exfiltration path
SELECT id, kind, platform, device_label, created_at, verified_at, last_success_at
FROM notification_channel ORDER BY created_at DESC;
```

**A notification channel you did not add is a strong signal.** It is the
easiest way for an attacker to exfiltrate data from Scout continuously.

**Check `device_label` and `platform` specifically.** A `native_push` row for a
device you do not own means someone installed the app against your account, and
they will keep receiving every notification until the row is deleted. Unlike a
session, a push registration survives an auth-token rotation — revoking
sessions alone does **not** stop it. Delete the channel row explicitly.

```bash
grep -E "Accepted|Failed|Invalid" /var/log/auth.log | tail -50
```

### Was there SSRF?

```
scout_ssrf_blocked_total          # should be nonzero if we blocked attempts
```

```bash
docker compose logs collector | grep -i "private\|metadata\|169.254\|localhost"
```

A blocked attempt is the system working. An outbound request that *succeeded* to
a private IP is a bypass and is critical.

### Were credentials leaked?

```bash
gitleaks detect --source . --verbose
git log -p --all -S "sk-" -S "AKIA" -S "postgres://" | head -100
```

Check LLM provider dashboards for usage from unexpected IPs.

### What data could have been reached?

If a session was compromised: everything that user can see — resume, application
history, interview notes, preferences.
If the database was reached directly: everything, though at rest it is on an
encrypted volume and channel credentials are additionally column-encrypted.
If only R2 was reached: resumes are client-side encrypted with a key that is not
in R2.

---

## Step 4 — Eradicate

**Rotate every credential that could have been exposed.**

```bash
# Database password
docker compose exec postgres psql -U postgres -c \
  "ALTER USER scout WITH PASSWORD '<new>';"
sops infra/secrets/production.enc.yaml    # update, save

# LLM API keys — revoke in each provider console first, then update secrets
# Session signing key
# Webhook HMAC secrets
# SSH keys — generate new, replace authorized_keys, remove old

docker compose up -d      # restart with new secrets
```

**Do NOT rotate the VAPID keypair** unless it is specifically what leaked — it
invalidates every Web Push subscription. **Do NOT rotate the backup encryption
key** — it orphans existing backups.

**Remove unauthorized artifacts:**

```sql
DELETE FROM webauthn_credential WHERE id = '<unauthorized>';
DELETE FROM notification_channel WHERE id = '<unauthorized>';
UPDATE session SET revoked_at = now() WHERE revoked_at IS NULL;
```

**Patch the vulnerability.** If the entry vector is unknown, assume the worst
case and harden broadly: update every dependency, rebuild every image, review
recent code for the class of flaw involved.

---

## Step 5 — Recover

```bash
docker compose up -d postgres redis
# verify integrity
docker compose up -d api web brain collector
# verify pipeline health for 10 minutes
docker compose up -d notifier
```

Re-register the passkey. Generate new recovery codes. Store them somewhere the
compromised system never touched.

---

## Step 6 — Verify

- [ ] All old sessions revoked
- [ ] Only expected passkeys exist
- [ ] Only expected notification channels exist
- [ ] Every credential rotated and old ones revoked at the provider
- [ ] Vulnerability patched and the patch verified
- [ ] No unexpected outbound connections (`ss -tunap`)
- [ ] No unexpected processes or cron entries
- [ ] Dependency scan clean
- [ ] `gitleaks` clean
- [ ] Fresh backup taken post-remediation

---

## Step 7 — Postmortem

Within 72 hours, in `docs/postmortems/`, covering: timeline, entry vector, data
potentially accessed, why detection took as long as it did, what contained it,
and concrete action items with owners and dates.

**Blameless.** The goal is a system that fails differently next time, not a
record of who erred.

---

## Specific playbooks

### Recovery code used unexpectedly

Someone with access to the recovery email attempted account takeover. Revoke
sessions, invalidate all remaining recovery codes, secure the email account
(this is the actual weak link), re-register the passkey, generate new codes.

### Request to a prohibited source

A compliance bug, not necessarily a security one — but treated as SEV2 because
the consequence is legal rather than technical.

```sql
SELECT id, url, kind, legal_posture, status FROM source WHERE id = '<id>';
```

Find how the gate was bypassed. This should be structurally impossible; if it
happened, the gate has a hole. Add a regression test before deploying the fix.

### Outbound request to a private IP

SSRF bypass. Critical. Find the URL that caused it, determine whether it reached
anything, add the case to the SSRF test corpus, and fix the resolver check.

---

## Prevention checklist (verify quarterly)

- [ ] SSH key-only, non-standard port, `fail2ban` active
- [ ] `ufw` default-deny inbound; only 80, 443, SSH open
- [ ] Postgres and Redis have no host port binding
- [ ] All containers non-root, read-only root filesystem, capabilities dropped
- [ ] `unattended-upgrades` active and applying
- [ ] Cloudflare origin certificate enforcement on
- [ ] Secrets encrypted at rest, none in Git history
- [ ] Backup encryption key stored separately from backups
- [ ] Recovery codes stored offline
- [ ] Dependency scanning green
