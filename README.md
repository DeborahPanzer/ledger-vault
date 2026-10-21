<div align="center">

# 🏛️ ledger-vault

> Immutable double-entry accounting ledger and balance reconciliation engine built for high-concurrency financial platforms.

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Architecture: Double-Entry | ACID](https://img.shields.io/badge/Architecture-Double--Entry_%7C_Serializable_ACID-2ea44f)](#)
[![Runtime: Go](https://img.shields.io/badge/Runtime-Go-00ADD8.svg)](#)
[![Sponsor](https://img.shields.io/badge/Sponsor-DeborahPanzer-ea4aaa?logo=github-sponsors&logoColor=white)](https://github.com/sponsors/DeborahPanzer)

</div>

---

# Double-Entry Ledger & Balance Vault Specification

**ledger-vault** serves as the foundational system of record connecting inbound payment processing (`checkout-core`), outbound disbursements (`payout-engine`), and dispute reversals (`refund-guard`). It enforces strict double-entry bookkeeping principles where every financial state mutation is recorded as an append-only, zero-sum journal entry.

## Core Architectural Guarantees

* **Zero-Sum Double-Entry Invariant:** Every transaction must consist of balanced debit and credit legs ($\sum \text{Debits} = \sum \text{Credits}$).
* **Append-Only Immutability:** Ledger entries are strictly immutable; state corrections require explicit contra-entries rather than `UPDATE` or `DELETE` operations.
* **ISO-4217 Minor Unit Precision:** All monetary values are processed as 64-bit integers (`int64`) in minor currency units (e.g., cents) to eliminate IEEE-754 floating-point rounding errors.
* **Serializable ACID Isolation:** Prevents phantom reads, write skew, and race conditions during concurrent balance locks.
* **Cryptographic Hash Chaining:** Each ledger entry computes a SHA-256 digest linking to the previous entry hash for tamper-evident auditability.

---

## Ledger Account Hierarchy

```text
                        ┌──────────────────────┐
                        │   Platform Chart     │
                        │     of Accounts      │
                        └──────────┬───────────┘
                                   │
         ┌─────────────────────────┼─────────────────────────┐
         ▼                         ▼                         ▼
┌─────────────────┐       ┌─────────────────┐       ┌─────────────────┐
│  Asset Accounts │       │Liability Account│       │ Revenue Account │
│ (Gateway Float) │       │(Merchant Escrow)│       │ (Platform Fees) │
└─────────────────┘       └─────────────────┘       └─────────────────┘

