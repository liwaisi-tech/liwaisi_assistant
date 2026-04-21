---
title: Brae tool_compose Policy — Host-Gate Class, Signed Manifests, HITL Surfacing
version: 0.2
date_created: 2026-04-20
last_updated: 2026-04-20
owner: liwaisi-tech / brae agent team
tags: [architecture, agent, security, host-gate, hitl, signed-manifest, toolhijacker, a2ui]
changelog:
  - "0.2 (2026-04-20): P0 panel-review fixes — dotted event names, expanded signed payload with key_version, builtin-only auto-approve (mixed-origin → hitl), key rotation/revocation, allow-list keyed by (intent_digest, sorted-qnames-sha256), realpath binary check, verification budget, persist-layer serialization constraint, hashtag normalization reference."

# Introduction

This specification defines the **`tool_compose` host-gate policy class**, the **signed-manifest provenance mechanism** that guards JIT composition against `ToolHijacker`-style adversarial tool descriptions, the **HITL card semantics** that ask the user before unsafe compositions instantiate, and the **A2UI surfacing** of compose-time activity. It addresses gaps **G8**, **G9**, and **G12** of the JIT CPN Builder design.

It does NOT redefine the existing host-gate policy classes (`safe`, `caution`, `dangerous`, `hitl`, `forbidden`, `introspection`); it adds one new class and one new approval surface.

## 1. Purpose & Scope

### Purpose

- Give operators a single policy knob to control when JIT composition may instantiate a sub-CPN: auto-approve when every involved tool is safe-band and signed, request HITL approval otherwise.
- Defend against tool-description tampering (ToolHijacker, arXiv 2504.19793) by signing every `ToolManifest` field that influences retrieval ranking, LLM judgement, or composition (`Toolbox`, `Summary`, `Hashtags`, `HelpText`, `ManPage`, `ArgSchema`, `SideEffectDescriptors`, `BinaryPath` content) and verifying signatures at compose time.
- Surface compose-time activity to the user through A2UI in a way that distinguishes "I am building tooling" from "I am running it" — the two are different trust events.
- Compose cleanly with the existing host-gate machinery; reuse, do not duplicate.

### In scope

- A new policy class `tool_compose` in `infra/host/policies/default.yaml`.
- A signed-manifest extension to `cpn.ProvenanceSnapshot` (signature + signing-key id + signing-key version + signed-payload digest).
- A `ManifestSigner` port and a default Ed25519 implementation.
- A verification step inside the JIT composer (`spec-architecture-brae-jit-cpn-builder.md` §4 SEC-002 + this spec).
- An A2UI HITL card variant for `tool_compose` approvals.
- Gate evaluation entry point in `infra/host/gate/policy_gate.go`.

### Out of scope

- Per-tool execution policy (the existing `dangerous`/`hitl` bands continue to apply when the spawned SubNet actually calls a tool).
- Key management beyond a signing-key registry referenced by ID. Operators run their own KMS.
- User-facing toolbox catalogue browser (deferred; the JIT path surfaces tools as it picks them, not on demand).

### Intended audience

Security/policy engineers, frontend engineers (A2UI HITL renderer), backend engineers (JIT composer integration).

### Assumptions

- The toolbox-taxonomy, retriever, tool-request-node, JIT composer, and awakening-extension specs are implemented or being implemented in parallel.
- The existing host-gate machinery (`infra/host/gate/`) accepts new classes by additive YAML edits without code changes for class declaration; new behaviour for `tool_compose` requires a small handler addition.
- The frontend already renders HITL cards for `dangerous` shell commands (`HostApprovalCard`); adding a variant is a small extension.

## 2. Definitions

| Term | Definition |
|---|---|
| **tool_compose** | New host-gate policy class evaluated when the JIT composer is about to spawn a sub-CPN. |
| **Signed manifest** | A `ToolManifest` whose `Hashtags + HelpText + ManPage` have been signed with a known key. |
| **ManifestSigner** | Port responsible for signing on registration and verifying on compose. |
| **ToolHijacker** | Adversarial pattern (arXiv 2504.19793) where attacker-controlled tool descriptions bias retrieval toward malicious tools. |
| **Compose decision** | One of `auto-approve | hitl | deny`. Result of evaluating the composed topology against the policy. |
| **HITL card (compose)** | A2UI card variant that previews the topology and asks the user to approve, deny, or remember. |
| **Key version** | Monotonic integer epoch tag for a signing keypair; bound into every signed payload so signatures can be invalidated en masse on rotation. |
| **Active key set** | Set of `(key_id, key_version)` pairs declared in `infra/host/sign/keys/active.yaml` that verify successfully. |
| **Revocation list** | Line-delimited list of revoked `key_id`s in `infra/host/sign/keys/revoked.txt`; verification against any revoked key MUST fail. |
| **Canonical JSON** | RFC 8785-style serialization: UTF-8, sorted object keys, no insignificant whitespace; used to hash structured fields into the signed payload. |

## 3. Requirements, Constraints & Guidelines

### Functional requirements

- **REQ-001**: A new policy class `tool_compose` MUST be added to `infra/host/policies/default.yaml` with the schema defined in §4.1.
- **REQ-002**: The JIT composer MUST call `gate.EvaluateCompose(ctx, ComposeRequest)` BEFORE the `instantiate` transition fires. The compose request includes the topology digest, the list of referenced tool qualified names, and per-tool origin/signed flags.
- **REQ-003**: The gate MUST return one of `auto-approve | hitl | deny`. The decision logic, in priority order:
  1. If any referenced tool has `Origin = "agent-authored"` AND `Verified = false`, return `deny`.
  2. If any referenced tool resides in a toolbox listed under `tool_compose.deny_toolboxes`, return `deny`.
  3. If every referenced tool's `Origin` is contained in `tool_compose.auto_approve_origins` (default `[builtin]`) AND every referenced tool is verified, return `auto-approve`.
  4. Otherwise return `hitl`. In particular, any topology containing at least one tool with `Origin ∈ {awakening, user, agent-authored}` MUST resolve to `hitl` under the default config, even when every manifest signature is valid. Operators MAY broaden the auto-approve set via `auto_approve_origins`; the security trade-off is documented in §7.
- **REQ-004**: A new `ManifestSigner` port MUST be defined: `Sign(payload []byte) (Signature, error)` and `Verify(payload []byte, sig Signature) (KeyID, KeyVersion, error)`. A default Ed25519 implementation MUST live in `infra/host/sign/`.
- **REQ-005**: `cpn.ProvenanceSnapshot` MUST gain four optional fields: `SignedDigest string` (hex sha256 of the signed payload), `Signature string` (base64), `SigningKeyID string`, `SigningKeyVersion uint32`.
- **REQ-006**: The signed payload format MUST be the byte sequence
  ```
  signed_v1\n<key_version>\n<ns>\n<name>\n<ver>\n<sha256(toolbox)>\n<sha256(summary)>\n<sha256(hashtags_canonical_json)>\n<sha256(help_text)>\n<sha256(man_page)>\n<sha256(arg_schema_canonical_json)>\n<sha256(side_effect_descriptors_canonical_json)>\n<binary_sha256_or_empty>
  ```
  where:
  - `<key_version>` is the monotonic integer epoch of the signing key, bound into the payload so a future rotation revokes legacy signatures atomically.
  - All structured fields (`hashtags`, `arg_schema`, `side_effect_descriptors`) are first serialized as **canonical JSON** (UTF-8, sorted object keys, no insignificant whitespace, RFC 8785-style) before SHA-256.
  - String fields (`toolbox`, `summary`, `help_text`, `man_page`) are SHA-256'd over their UTF-8 byte content.
  - `<binary_sha256_or_empty>` is the lowercase hex SHA-256 of the binary referenced by `BinaryPath` after **realpath resolution** (see SEC-008), or the empty string for tools without a binary.
  Each `\n` is a single 0x0A byte. The signer signs the payload bytes; the verifier re-derives them from the live registry row and the live binary file.
- **REQ-007**: At registration time (`Registry.RegisterEntry`), if a `ManifestSigner` is configured AND the manifest's `Origin ∈ {builtin, awakening, user}`, the registry MUST sign the manifest and persist the signature fields. `agent-authored` manifests are NOT auto-signed.
- **REQ-008**: At compose time, the composer MUST call `signer.Verify` for every referenced manifest. Verification failure MUST mark the tool `Verified = false` for the policy decision.
- **REQ-009**: When `EvaluateCompose` returns `hitl`, the parent transition MUST emit an A2UI HITL card per §4.4 and block on user response.
- **REQ-010**: A user response of `approve-and-remember` MUST persist a session-scoped allow entry keyed by `(intent_digest, sorted_qualified_names_sha256)` — where `sorted_qualified_names_sha256` is the SHA-256 of the lexicographically-sorted, newline-joined list of qualified names actually referenced by the topology. On every subsequent `EvaluateCompose`, the gate MUST recompute `sorted_qualified_names_sha256` from the live tool set and reject (i.e. ignore, falling through to the normal decision logic) any cached approval whose hash does not match. This closes the confused-deputy vector where a topology digest is reused across differing tool sets.
- **REQ-011**: The HITL card MUST include the topology digest, the matched tools (qualified names + scores), and a clear distinction between signed/builtin and unsigned tools.
- **REQ-012**: Hashtag normalization MUST follow `spec-architecture-brae-toolbox-taxonomy.md` REQ-NORM-001; this spec MUST NOT redefine it. The signed-payload step in REQ-006 takes the already-normalized `hashtags` array as input to canonical-JSON serialization.
- **REQ-013**: All events emitted by this gate MUST follow the canonical lowercase `<domain>.<subject>.<verb>` dot-form mandated by `spec-architecture-brae-toolbox-taxonomy.md` (event-naming requirement). See §4.6 for the catalogue.

### Security requirements

- **SEC-001**: Signing keys MUST live in `infra/host/sign/keys/` (or via an environment-variable indirection); they MUST NOT be checked into version control. The default Ed25519 keypair generated at first boot is acceptable for dev; production deployments MUST configure their own.
- **SEC-002**: Verification MUST be constant-time. Failed verifications MUST log the tool qualified name and the failure reason but MUST NOT log the signature or payload bytes.
- **SEC-003**: An unverified `agent-authored` tool referenced in a JIT topology MUST cause `deny` regardless of any user approval. Approve-and-remember does NOT override the unverified-author rule.
- **SEC-004**: The HITL card MUST clearly mark unsigned tools with a visible warning glyph; it MUST NOT bury this in tooltips or aria-only text.
- **SEC-005**: The session-scoped allow list MUST be cleared on session destruction. It MUST NOT persist across sessions.
- **SEC-006**: The gate MUST NEVER auto-approve when the composed topology references a tool whose `BinaryPath` SHA256 has drifted (existing `VerifyBinary` check on registry); a binary-drift failure short-circuits to `deny` even if the manifest is signed.
- **SEC-007**: The retriever's traces (from `spec-architecture-brae-tool-retriever.md`) MUST be sanitised so that no manifest field resembling a prompt-injection payload is rendered into the HITL card without escaping.
- **SEC-008**: Binary verification MUST resolve symlinks via `filepath.EvalSymlinks` (realpath) on the configured `BinaryPath` BEFORE computing `binary_sha256`, both at registration and on every `EvaluateCompose`. A symlink swap that changes the resolved target between registration and evaluation MUST be detected as drift, MUST emit `manifest.binary.drift` (with old resolved path, new resolved path, and qualified name) and MUST force the decision to `deny`. Symlink swap is a known sandbox-bypass vector; the realpath check is mandatory.
- **SEC-009**: Signature verification MUST run synchronously inside `EvaluateCompose`, **before** any lint or structural checks, so a tampered manifest cannot influence later compose-time decisions even partially. Per-tool verification budget ≤ 500 µs; total verification budget ≤ 20 ms for a topology of up to 100 tools. On budget violation the gate MUST emit `manifest.verification.timeout` (with measured nanoseconds and tool count) and MUST force the decision to `deny`. Budget violation is a soft DoS signal and a hard refusal posture.
- **SEC-010**: Signing keys are versioned. The verifier MUST resolve `(SigningKeyID, SigningKeyVersion)` against the active key set declared in `infra/host/sign/keys/active.yaml`. A key id that does not appear in the active set, OR appears in `infra/host/sign/keys/revoked.txt`, MUST cause verification to fail, MUST emit `manifest.key.revoked` (with `key_id`, `key_version`, qualified name) and MUST mark the tool `Verified = false`.

### Behaviour & product requirements

- **BEH-001**: The HITL card MUST render in Spanish or English depending on the session's locale (existing i18n machinery).
- **BEH-002**: The A2UI activity bubble (from the tool-request spec) MUST update to indicate when composition is awaiting HITL approval (`state: "awaiting-approval"`).
- **BEH-003**: When the gate returns `deny`, the parent transition MUST emit a polite user-facing message ("This combination of tools needs operator approval; I cannot continue with this request") AND a `tool_compose_denied` event with the structured reason.

### Constraints

- **CON-001**: Compose-decision evaluation MUST run in ≤ 10 ms p95 (excluding HITL wait time).
- **CON-002**: Signature verification MUST run in ≤ 200 µs per manifest (Ed25519 budget).
- **CON-003**: Approve-and-remember entries MUST be capped at 32 per session; older entries evict LRU.
- **CON-004**: The gate MUST NOT call out to network services. Verification is purely local-key.
- **CON-005**: Signature verification budget — per-tool ≤ 500 µs; whole-topology ≤ 20 ms for ≤ 100 tools (see SEC-009 for the failure mode).
- **CON-006**: `cpn.ProvenanceSnapshot` is data-only. Serialization and deserialization of the signature/key-version fields to/from durable storage is the responsibility of the `persist/` layer. The `cpn/` package MUST NOT import `persist/` for this round-trip; the `persist/` adapter owns the schema mapping. This preserves hexagonal direction (domain has no infrastructure dependency).

### Guidelines

- **GUD-001**: Operators SHOULD list `agent-authored` toolboxes in `tool_compose.hitl_toolboxes` so even verified agent-authored tools surface a HITL card. The default config does this.
- **GUD-002**: Avoid putting `general` in `deny_toolboxes`; the long-tail of small utilities is too useful to block by default.
- **GUD-003**: The HITL card SHOULD show no more than 5 tools by default; if the topology references more, present a "show all" expansion.

### Patterns

- **PAT-001**: The compose gate is a single function call before instantiation. No new transitions, no new node kinds.
- **PAT-002**: Approve-and-remember reuses the existing per-session HITL persistence layer, scoped by a different key namespace.
- **PAT-003**: Signature verification is layered, not gated: an unverified manifest doesn't block compose by itself; it changes the policy decision.

## 4. Interfaces & Data Contracts

### 4.1 `tool_compose` policy block (`infra/host/policies/default.yaml`)

```yaml
tool_compose:
  description: Governs JIT CPN composition before sub-CPN instantiation.
  default_decision: hitl
  auto_approve_origins: [builtin]   # default: builtin-only. Operators MAY add awakening
                                    # at their own risk; see §7 trade-off.
  auto_approve_when:
    all_origins_in_auto_approve_origins: true
    all_signed: true
  deny_when:
    any_origin_in: [agent-authored]
    any_unsigned: true
    binary_drift: true
    verification_timeout: true
    key_revoked: true
  deny_toolboxes: []         # empty by default
  hitl_toolboxes:
    - "agent-authored:*"      # placeholder syntax to flag any agent-authored toolbox
  remember_within_session: true
  remember_cap: 32            # LRU; key = (intent_digest, sorted_qnames_sha256)
```

### 4.2 `ManifestSigner` port (`cpn/sign_port.go`)

```go
type ManifestSigner interface {
    Sign(payload []byte) (Signature, error)
    // Verify returns the resolved KeyID and KeyVersion on success. It MUST
    // fail when the key is absent from the active key set or appears in the
    // revocation list (see SEC-010).
    Verify(payload []byte, sig Signature) (KeyID, KeyVersion, error)
}

type Signature struct {
    KeyID      string
    KeyVersion uint32
    Bytes      []byte
}

type KeyID string
type KeyVersion uint32
```

### 4.3 Extended `ProvenanceSnapshot`

```go
type ProvenanceSnapshot struct {
    AuthoringCPNID string
    FlowHash       string
    ForgeRunID     string
    PromptDigest   string
    SourcePath     string
    SourceSHA256   string

    // ── new in this spec ──
    // NOTE: these fields are data-only. JSON tags shown for documentation,
    // but the cpn/ package MUST NOT import persist/ or perform DB
    // serialization itself; the persist/ adapter owns the round-trip
    // (see CON-006).
    SignedDigest      string `json:"signed_digest,omitempty"`
    Signature         string `json:"signature,omitempty"`
    SigningKeyID      string `json:"signing_key_id,omitempty"`
    SigningKeyVersion uint32 `json:"signing_key_version,omitempty"`
}
```

### 4.4 A2UI HITL card (compose variant)

```json
{
  "components": [
    { "type": "card",
      "props": {
        "variant": "warning",
        "title": "brae quiere componer una herramienta nueva"
      },
      "children": [
        { "type": "text", "props": {
          "content": "Para tu pregunta «extract text from a scanned PDF» propongo combinar:" } },
        { "type": "list", "children": [
          { "type": "list-item", "props": {
            "label": "pdf/pdf-to-text@0.1.0", "badge": "✓ firmado · builtin", "score": 0.87 }},
          { "type": "list-item", "props": {
            "label": "pdf/ocr@0.1.0", "badge": "✓ firmado · awakening", "score": 0.71 }}
        ]},
        { "type": "divider" },
        { "type": "text", "props": {
          "content": "Topología: jit-9f1c7a... (4 lugares, 4 transiciones)" }},
        { "type": "actions", "children": [
          { "type": "button", "props": { "kind": "approve",            "label": "Aprobar" }},
          { "type": "button", "props": { "kind": "approve-and-remember","label": "Aprobar y recordar" }},
          { "type": "button", "props": { "kind": "deny",               "label": "Cancelar" }}
        ]}
      ]
    }
  ]
}
```

When unsigned tools are referenced, their badge becomes `⚠ no firmado` and the card variant becomes `danger`.

### 4.5 Gate API (`infra/host/gate/policy_gate.go`)

```go
type ComposeRequest struct {
    SessionID         string
    TopologyDigest    string
    IntentDigest      string
    Tools             []ComposeTool
    BinaryDriftFlags  map[string]bool   // qualified_name -> drifted?
}

type ComposeTool struct {
    QualifiedName string
    Toolbox       string
    Origin        string
    Verified      bool
}

type ComposeDecision struct {
    Outcome string  // "auto-approve" | "hitl" | "deny"
    Reason  string
    DenyTools []string  // qualified names triggering deny
}

func (g *PolicyGate) EvaluateCompose(ctx context.Context, req ComposeRequest) ComposeDecision
```

### 4.6 Events

All event names follow the canonical lowercase `<domain>.<subject>.<verb>` dot-form mandated by `spec-architecture-brae-toolbox-taxonomy.md`. See REQ-013.

```json
{"event":"tool_compose.evaluated","session_id":"...","topology_digest":"...","outcome":"hitl","reason":"agent-authored tool present"}
{"event":"tool_compose.approved","topology_digest":"...","persisted":true}
{"event":"tool_compose.denied","topology_digest":"...","reason":"unsigned agent-authored tool: dev/foo@0.1.0"}
{"event":"manifest.verification.failed","qualified_name":"...","reason":"signature mismatch"}
{"event":"manifest.verification.timeout","qualified_name":"...","measured_ns":612400,"tool_count":104}
{"event":"manifest.binary.drift","qualified_name":"...","old_realpath":"/usr/bin/grep","new_realpath":"/tmp/evil"}
{"event":"manifest.key.revoked","qualified_name":"...","key_id":"brae-2026-q1","key_version":3}
```

### 4.7 Key Rotation Procedure

Signing keys are versioned. Every signature carries a `KeyVersion` (REQ-005, §4.2) and the version is bound into the signed payload (REQ-006). The rotation lifecycle:

1. **Active key set.** `infra/host/sign/keys/active.yaml` enumerates the currently-acceptable signing keys, e.g.
   ```yaml
   keys:
     - id: brae-2026-q1
       version: 3
       algorithm: ed25519
       public_key_path: ./brae-2026-q1.v3.pub
   ```
   The verifier resolves `(KeyID, KeyVersion)` against this file. A pair that is absent MUST cause verification failure (SEC-010).
2. **Revocation.** `infra/host/sign/keys/revoked.txt` is a line-delimited list of revoked `key_id` values. A signature whose `KeyID` appears here MUST fail verification, MUST emit `manifest.key.revoked`, and MUST mark the tool `Verified = false`.
3. **Re-signing automation.** A `make resign-all-tools` target MUST exist. It iterates every row in `tool_registry`, recomputes the canonical signed payload (REQ-006) using the current active key, writes the new `Signature`, `SignedDigest`, `SigningKeyID`, `SigningKeyVersion` via the `persist/` adapter, and emits one `tool_compose.evaluated` synthetic dry-run per row to confirm acceptance.
4. **Operator workflow on rotation.**
   1. Generate the new keypair; install the public key alongside the old one in `active.yaml` with the next monotonic `version`.
   2. Run `make resign-all-tools` against the new key.
   3. Add the previous `key_id` to `revoked.txt` after re-signing has been verified end-to-end.
   4. Remove the previous key entry from `active.yaml` once at least one full deployment cycle has confirmed no straggling signatures remain.
5. **Failure mode: lost signing key.** If the active signing key material is lost, the operator MUST: (a) generate a fresh keypair, (b) bump the active key version, (c) run `make resign-all-tools` to mint fresh signatures over every existing manifest, (d) add the lost key id to `revoked.txt`. During the migration window, tools whose signatures could not yet be re-minted will resolve to `Verified = false` and the gate will fall through to its normal `hitl`/`deny` posture — there is no soft-acceptance escape hatch for unverified manifests.

## 5. Acceptance Criteria

- **AC-001**: Given a composed topology referencing only `builtin` signed tools and the default `auto_approve_origins: [builtin]`, when `EvaluateCompose` is called, then the decision MUST be `auto-approve` AND no HITL card MUST be emitted.
- **AC-002**: Given a composed topology referencing one `agent-authored` tool that IS verified, when evaluated, then the decision MUST be `hitl` AND a HITL card MUST be emitted.
- **AC-003**: Given a composed topology referencing one `agent-authored` tool that is NOT verified, when evaluated, then the decision MUST be `deny` regardless of any prior approval.
- **AC-004**: Given the user clicks `approve-and-remember` on a HITL card, when an identical composition (same `intent_digest` AND identical sorted-qualified-names set) recurs in the same session, then `EvaluateCompose` MUST return `auto-approve` AND no card MUST be shown the second time.
- **AC-005**: Given the same composition recurs in a NEW session, when evaluated, then the cached approval MUST NOT carry over AND the card MUST be shown again.
- **AC-006**: Given a tool's BinaryPath SHA256 (computed after realpath) has drifted, when included in a compose request, then the decision MUST be `deny` even if the manifest signature is valid AND `manifest.binary.drift` MUST be emitted.
- **AC-007**: Given a manifest is registered with a configured signer, when read back, then `Provenance.Signature`, `Provenance.SignedDigest`, `Provenance.SigningKeyID` AND `Provenance.SigningKeyVersion` MUST be populated.
- **AC-008**: Given a manifest's `HelpText` is mutated after registration, when verified, then verification MUST fail AND `manifest.verification.failed` MUST be emitted.
- **AC-009**: Given the user clicks `deny`, when the response is processed, then the parent transition MUST emit `tool_compose.denied` AND emit a polite user-facing message AND the SubNet MUST NOT spawn.
- **AC-010**: Given the activity bubble is in `state: "running"`, when `EvaluateCompose` returns `hitl`, then the bubble MUST update to `state: "awaiting-approval"` BEFORE the HITL card is rendered.
- **AC-011**: Given the default config (`auto_approve_origins: [builtin]`) AND a topology that mixes a signed `builtin` tool with a signed `awakening` tool, when evaluated, then the decision MUST be `hitl` AND a HITL card MUST be emitted (mixed-origin → ToolHijacker mitigation).
- **AC-012**: Given an operator opts in by setting `auto_approve_origins: [builtin, awakening]` AND a topology referencing only signed `builtin`+`awakening` tools, when evaluated, then the decision MUST be `auto-approve`.
- **AC-013**: Given a manifest signed with key version `2` AND `infra/host/sign/keys/active.yaml` lists only key version `3`, when verified, then verification MUST fail, `manifest.key.revoked` MUST be emitted, and the tool MUST be marked `Verified = false`.
- **AC-014**: Given a key id appears in `infra/host/sign/keys/revoked.txt`, when any manifest signed by that key is verified, then verification MUST fail AND `manifest.key.revoked` MUST be emitted with `key_id` and `key_version`.
- **AC-015**: Given an approve-and-remember entry exists keyed by `(intent_digest, sorted_qnames_sha256_A)`, when an evaluation arrives with the same `intent_digest` but a different qualified-name set hashing to `sorted_qnames_sha256_B`, then the cached approval MUST NOT apply AND the gate MUST fall through to the normal decision logic (typically `hitl`).
- **AC-016**: Given a `BinaryPath` that is a symlink, when the symlink target is swapped between registration and evaluation (registered target SHA differs from the realpath SHA at evaluation), then the decision MUST be `deny` AND `manifest.binary.drift` MUST be emitted with both resolved paths.
- **AC-017**: Given a topology of 100 tools where signature verification is artificially slowed past the 20 ms whole-topology budget, when `EvaluateCompose` runs, then the decision MUST be `deny` AND `manifest.verification.timeout` MUST be emitted with `measured_ns` and `tool_count`.
- **AC-018**: Given the JIT composer's compose pipeline, when `EvaluateCompose` runs, then signature verification MUST execute synchronously before any lint/structural checks (verifiable via ordered call assertions in the integration test).

## 6. Test Automation Strategy

- **Test levels**: Unit (decision logic, signer round-trip, payload format), Integration (composer + gate against in-memory registry), End-to-End (HTTP turn driving compose → HITL → approve → SubNet spawn).
- **Frameworks**: Go `testing` stdlib; deterministic Ed25519 keypair fixture for repeatable signature tests; Vitest + RTL for the HITL card variant; Playwright for the full E2E (issue intent, see card, click approve, see SubNet result).
- **Test data**: Fixtures under `infra/host/gate/testdata/compose/` covering every decision arm; fixtures with deliberately-tampered help text for verification failure.
- **CI/CD integration**: Standard `go test ./...`. A separate signing-key generation step in CI seeds a fixed keypair so signature bytes are reproducible.
- **Coverage requirements**: ≥ 92 % line coverage on the gate decision logic; ≥ 90 % on the signer.
- **Performance testing**: Benchmark `EvaluateCompose` and `Verify`. Assert CON-001 and CON-002.
- **Security testing**: Adversarial fixtures: a manifest signed with the wrong key, a manifest where the signed payload differs from the live row, a topology referencing 100 tools (bound test), a HelpText containing prompt-injection markers (rendering test).

## 7. Rationale & Context

The JIT CPN Builder turns retrieval into composition into execution in three steps. Each step expands the trust surface: retrieval is read-only, composition is structural, execution is consequential. The existing host-gate already governs execution per command; this spec adds the missing gate at the *composition* boundary. Without it, a malicious tool description (ToolHijacker) could bias the retriever toward an attacker-controlled tool, then bias the composer toward including it in a sub-CPN, with the user only ever seeing the eventual command-level approvals — which arrive too late to reason about whether the *combination* was the right idea in the first place.

Signed manifests are the cheapest defence against retrieval-time tampering. Ed25519 is fast (≤ 200 µs verify), small (64-byte signatures), and well-understood. The expanded signed-payload set (`Toolbox`, `Summary`, `Hashtags`, `HelpText`, `ManPage`, `ArgSchema`, `SideEffectDescriptors`, `binary_sha256`) covers every field that biases retrieval ranking, LLM judgement, OR composition wiring — closing the v0.1 gap where an attacker who could mutate `Summary` / `ArgSchema` post-registration could still bias the system without invalidating signatures. Administrative fields like `RegisteredBy` remain outside the payload so housekeeping doesn't invalidate signatures. Binding `<key_version>` into the payload (REQ-006) means rotation atomically invalidates legacy signatures: an attacker who captured an old key cannot replay forgeries past the next rotation. The signed-payload format remains plain-text auditable.

**Why the default is builtin-only auto-approve.** Even with verified signatures, a tool packaged via the awakening pipeline lives in a wider supply chain than `builtin`: it was discovered, pulled, and registered in response to runtime stimuli. ToolHijacker-style attacks that compromise the awakening intake — not the signature step — would still surface as a "verified" tool. Restricting auto-approve to `builtin` (the canonical, in-tree set) means at least one human-reviewed gate sits between the awakening intake and silent compose-time approval. Operators who accept this risk MAY broaden via `auto_approve_origins: [builtin, awakening]`; the explicit opt-in makes the trade-off visible in config review. `user` and `agent-authored` origins remain off the auto-approve table by design.

The HITL surface is deliberately distinct from the existing dangerous-command HITL. Composition is a different question: "do you want me to wire these tools together?" is a different ask than "do you want me to run `rm -rf foo`?". Conflating them confuses the user. The compose card shows the topology digest and the constituent tools so the user can recognise the pattern; approve-and-remember handles the loop case where the same composition recurs in a session (a common UX problem in agentic systems where users tire of clicking approve).

Deny on unverified-author is intentionally non-overridable. If we let users say "approve anyway", we make the verification step ceremonial. Operators can disable signing entirely via configuration if they accept the risk; what they cannot do is use the approval surface to launder unverified manifests through. This is the same posture as the binary-drift check: some failure modes are not negotiable.

## 8. Dependencies & External Integrations

### External systems

- **EXT-001**: None. All cryptographic operations are local.

### Third-party services

- **SVC-001**: PostgreSQL — for persisting signature fields on `tool_registry.provenance` (additive JSONB inside the existing `provenance` column, no migration needed).

### Infrastructure dependencies

- **INF-001**: Existing `infra/host/gate/policy_gate.go` and YAML loader.
- **INF-002**: Existing HITL workflow in `infra/host/gate/hitl.go` and Redis-backed pending-request store. New card variant adds a new approval class.
- **INF-003**: Existing event bus and SSE fan-out.

### Data dependencies

- **DAT-001**: Signing key material at the path or env var configured by `BRAE_SIGNING_KEY` (default `infra/host/sign/keys/default.ed25519`).
- **DAT-002**: Tool registry rows with `provenance` JSONB column (existing).

### Technology platform dependencies

- **PLT-001**: Go 1.25+ with `crypto/ed25519` (stdlib).
- **PLT-002**: A2UI v0.8 supporting `card` variant=`warning`/`danger` and `actions` with `approve-and-remember`.

### Compliance dependencies

- **COM-001**: Operators must store signing keys with appropriate access control. The spec does not mandate KMS but recommends one for production deployments.

## 9. Examples & Edge Cases

### 9.1 Auto-approve

```
topology references: [system/grep@0.1.0 (builtin, signed), system/awk@0.1.0 (builtin, signed)]
decision: auto-approve
event: tool_compose.evaluated outcome=auto-approve
result: SubNet spawns; no card
```

### 9.2 HITL with all-signed mix

```
topology references: [pdf/pdf-to-text@0.1.0 (builtin, signed),
                      awakens/sqlite-query@0.1.0 (awakening, signed),
                      developer/git-blame@0.1.0 (agent-authored, signed)]
decision: hitl
event: tool_compose.evaluated outcome=hitl reason="non-builtin origin under default auto_approve_origins=[builtin]"
card: warning variant; user approves; user clicks "approve-and-remember"
session-store: {(intent_digest, sorted_qnames_sha256): "approved"}
```

Note: under the default `auto_approve_origins: [builtin]`, even a topology that mixes only signed `builtin`+`awakening` tools resolves to `hitl`. This is the mixed-origin ToolHijacker mitigation (AC-011).

### 9.3 Deny on unverified

```
topology references: [foo/bar@0.1.0 (agent-authored, NOT signed)]
decision: deny
event: tool_compose.denied reason="unsigned agent-authored tool: foo/bar@0.1.0"
SubNet: not spawned
user message: "Esta combinación incluye una herramienta sin firma confiable; no puedo proceder."
```

### 9.4 Deny on binary drift

```
topology references: [system/grep@0.1.0 (builtin, signed, BUT binary SHA256 differs)]
decision: deny (binary drift)
event: tool_compose.denied reason="binary drift: system/grep@0.1.0"
```

### 9.5 Edge case: signer not configured

If `BRAE_SIGNING_KEY` is unset, no manifest is signed at registration and `Verified = false` for every tool. The gate then defaults to `hitl` for everything (since `auto_approve_when.all_signed = true` cannot hold even with the default `auto_approve_origins: [builtin]`). Operators in this mode see a HITL card on every JIT composition. A startup log warns `manifest_signer=disabled`.

### 9.6 Edge case: card timeout

If the user neither approves nor denies within the existing HITL timeout (1 hour, configurable), the parent transition treats it as `deny` and emits `tool_compose.denied reason="hitl_timeout"`.

### 9.7 Edge case: tampered help text

A row in `tool_registry` is altered out-of-band (e.g. via a SQL injection that bypassed the application). Next compose: `Verify` fails for that row; gate flips the decision to `deny`; `manifest.verification.failed` event fires; admin alerted.

### 9.8 Edge case: symlink swap

A tool's `BinaryPath` is `/opt/brae/bin/grep`, a symlink to `/usr/bin/grep` at registration. An attacker re-points the symlink to `/tmp/evil`. Next compose: the realpath check (SEC-008) resolves to `/tmp/evil`, the recomputed `binary_sha256` differs from the signed payload's `<binary_sha256_or_empty>` field, the gate flips to `deny`, and `manifest.binary.drift` fires with both resolved paths.

### 9.9 Edge case: key rotation in flight

After `make resign-all-tools` runs, a small set of legacy signatures (e.g. tools registered concurrently during the rotation window) remain pinned to the old key version. Their next `EvaluateCompose` produces `manifest.key.revoked` and `Verified = false`. The operator either re-runs the resign target or tolerates the per-tool HITL prompts until the next rotation cycle.

### 9.10 Edge case: confused-deputy allow-list

Session has an `approve-and-remember` entry for `(intent_digest_X, sorted_qnames_sha256_A)` covering tools `[a, b, c]`. A later turn produces a topology with the same `intent_digest_X` but tools `[a, b, d]` (a malicious substitution). The recomputed hash is `sorted_qnames_sha256_B ≠ sorted_qnames_sha256_A`, the cached approval is ignored, and the gate falls through to `hitl` (or `deny` if `d` is unverified).

## 10. Validation Criteria

- All eighteen Acceptance Criteria (AC-001 … AC-018) pass.
- Bench `EvaluateCompose` ≤ 10 ms p95; `Verify` ≤ 200 µs per manifest; whole-topology verification ≤ 20 ms for 100 tools (CON-002, CON-005).
- Key rotation rehearsal: `make resign-all-tools` re-mints signatures over a fixture registry, the previous key id appears in `revoked.txt`, and a subsequent `EvaluateCompose` against pre-rotation signatures produces `manifest.key.revoked`.
- Symlink-swap regression test: registering a tool against a symlinked binary then swapping the target produces `manifest.binary.drift` and `deny`.
- Adversarial-fixtures suite covers every deny path and produces the documented event payloads.
- The HITL card renders in both supported locales; a Playwright walk-through approves a composition end-to-end.
- A startup log entry confirms whether the signer is enabled and which key id is in use; absent signer logs WARN.

## 11. Related Specifications / Further Reading

- `spec/spec-architecture-brae-toolbox-taxonomy.md`
- `spec/spec-architecture-brae-tool-retriever.md`
- `spec/spec-architecture-brae-tool-request-node.md`
- `spec/spec-architecture-brae-jit-cpn-builder.md`
- `spec/spec-architecture-brae-awakening-toolbox-extension.md`
- ToolHijacker (2025), arXiv 2504.19793 — adversarial threat model.
- Borghoff, Bottoni, Pareschi (2025), arXiv 2502.14000.
- TB-CSPN, MDPI Future Internet, doi:10.3390/fi17080363.

## 12. Search Keywords

tool_compose, host-gate, signed manifest, ed25519, ToolHijacker, HITL card, approve-and-remember, manifest verification, JIT composition gate, A2UI compose card, brae, security, hexagonal, key rotation, key revocation, key version, canonical JSON, realpath, symlink swap, binary drift, verification timeout, mixed-origin, auto_approve_origins, confused deputy.
