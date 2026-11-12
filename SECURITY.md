# Ledger Vault Security & Immutability Policy

`ledger-vault` serves as the **Immutable Double-Entry System of Record** for the financial core ecosystem (`checkout-core`, `payout-engine`, and `refund-guard`). Security, cryptographic auditability, and **SOX Section 404 / GAAP** conservation invariants are treated as P0 architectural guarantees.

---

## Supported Versions

| Release Stream | Go Runtime | Security & Cryptographic Patches |
| :--- | :--- | :--- |
| `v1.2.x` | Go `1.22+` | Actively Supported |
| `v1.1.x` | Go `1.22+` | Critical Fixes Only |
| `< v1.1.0` | Legacy | End of Life |

---

## Core Security Invariants

1. **Strict Append-Only Storage:** `UPDATE` and `DELETE` mutations against committed `journal_entries` and `ledger_postings` are strictly prohibited at both the Go domain layer and PostgreSQL trigger layer. Corrections MUST be executed exclusively via symmetric reversing entries.
2. **Zero Floating-Point Arithmetic:** All balances and journal postings are represented strictly as 64-bit integer minor units (`int64`) validated against ISO-4217 exponents with checked overflow protection.
3. **Cryptographic Tamper Evidence:** Every committed journal entry links to its predecessor via deterministic **SHA-256 hash-chaining** and daily **Merkle inclusion proofs**.
4. **Constant-Time Ecosystem Authentication:** Inbound `FinancialEventEnvelope` payloads from upstream services are verified using `crypto/hmac.Equal` and bounded 300-second replay windows.

---

## Reporting a Vulnerability

Please **do not** open public GitHub issues for suspected conservation bypasses, lock-ordering deadlocks, hash-chain collisions, or SQL isolation anomalies.

* **Disclosure Channel:** Report privately via GitHub **Security Advisories** (`Security` tab $\rightarrow$ `Report a vulnerability`).
* **Acknowledgement SLA:** Within **24 hours** for P0 ledger conservation or isolation issues.
* **Remediation Target:** Within **72 hours** accompanied by a regression test and TLA+ invariant review.
