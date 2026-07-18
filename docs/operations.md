# Operations Guide

Day-2 operation of the Netboot Manager.

## Enabling the DHCP service

The authoritative DHCP server is **disabled by default** and never starts on
its own (constitution: Secure by Default; FR-016). It is the highest-risk
component — an errant DHCP server disrupts every machine on the segment — so
enabling it is always an explicit operator action.

1. Confirm the daemon is on an **isolated provisioning network**. Universe's
   DHCP is authoritative and will answer every valid request on its segment.
2. In the UI: **DHCP → configure at least one subnet** (network CIDR, range,
   gateway, DNS) and save. Invalid configuration is rejected at save time and
   the running server keeps its last valid configuration.
3. Toggle **Enable DHCP** and confirm the dialog. The runtime controller starts
   the server immediately; disabling stops it. Configuration changes while
   enabled hot-reload the server.

Equivalent API calls: `PUT /api/v1/dhcp/config`, then `POST /api/v1/dhcp/enable`
(`/disable`).

## Handling foreign-DHCP conflicts

Universe passively watches for DHCP OFFER/ACK packets from other server IDs on
the segment and records them. If the **conflicts banner** appears in the DHCP
page (or `GET /api/v1/dhcp/conflicts` returns rows):

- Another DHCP server is active on the same L2 segment. Two authoritative DHCP
  servers will race and hand out conflicting addresses.
- Either move Universe to a dedicated provisioning VLAN, or decommission the
  other server. Universe does **not** run in proxy-DHCP mode in v1.

## Provisioning a machine

1. **Boot Files**: upload the Ubuntu kernel and initrd for the target release
   (jammy/noble). SHA-256 is computed and shown.
2. **Profiles**: create a profile (release, storage layout, SSH keys, packages,
   late-commands). Save-time validation renders the profile and rejects it if it
   would not produce a valid autoinstall document.
3. **Machines**: register the machine by MAC, assign the profile, and click
   **Provision**. This opens a session and arms the next boot (requires DHCP
   enabled). Network-boot the machine; watch the live timeline under
   **Sessions**.
4. The installer phones home on completion; the session moves to `completed` and
   the machine to `installed`. Failures are captured with the last completed
   phase and evidence for diagnosis without console access.

Unknown (unregistered) MACs that attempt to boot are denied and recorded under
**Machines → Unknown boots**, where they can be registered in one click.

## Tuning event retention

Provisioning events, TFTP transfers, and foreign-offer records are TimescaleDB
hypertables with a retention policy (default **90 days**). Adjust
`events.retention_days` in the config; the value seeds the migration's
`add_retention_policy`. To change retention on a running database:

```sql
SELECT remove_retention_policy('provisioning_events');
SELECT add_retention_policy('provisioning_events', INTERVAL '30 days');
```

Repeat for `tftp_transfers` and `dhcp_offers_seen`.

## Stale sessions

A background sweeper marks provisioning sessions that have been `active` longer
than `netboot.stale_session_timeout` (default 60m) as `stale` and fails their
machines, capturing the last completed phase. Tune the timeout to your slowest
expected install.

## End-to-end netboot test (QEMU)

`make test-e2e` drives a real unattended install in a VM on a host-only bridge
(BIOS and UEFI/OVMF). It requires KVM and `qemu-system-x86_64` + `ovmf`. The
harness scripts live under `backend/tests/e2e/scripts/`. This is the only test
that exercises firmware PXE ROMs end-to-end; run it before releasing changes to
the DHCP/TFTP/boot-HTTP path.
