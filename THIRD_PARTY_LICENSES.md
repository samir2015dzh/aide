# Third-Party Licenses and Attributions

Aide uses and references the following open-source projects. Only permissive
licenses (MIT / Apache-2.0 / BSD) are used; copyleft (GPL/AGPL) components are
strictly excluded.

## ACP Protocol & Registry (#110)

- **coder/acp-go-sdk** (Apache-2.0) — https://github.com/coder/acp-go-sdk
  Official Go client/protocol SDK reference for Agent Client Protocol (ACP).
- **agentclientprotocol/registry** (Apache-2.0) — https://github.com/agentclientprotocol/registry
  Official ACP Registry catalog schema, platform distribution metadata, and
  bundled fallback snapshot (`internal/acpregistry/snapshot.json`).
- **pingdotgg/t3code** (MIT) — https://github.com/pingdotgg/t3code
  Architectural reference for generic ACP provider layering, v2/v1 protocol
  negotiation, SHA-256 verified registry installs, and truthful capability
  declarations.
  Copyright (c) Ping Labs / t3code contributors — MIT License.

## Open Model Metadata Catalog (#114)

- **anomalyco/models.dev** (MIT) — https://github.com/anomalyco/models.dev
  Open model metadata catalog (context window, max output tokens, capabilities).
- **BerriAI/litellm** (MIT) — https://github.com/BerriAI/litellm
  `model_prices_and_context_window.json` pricing and token-limit reference.

## Session Checkpoints & Weak-Network Recovery (#113, #115)

- **entireio/cli** (MIT) — https://github.com/entireio/cli
  Design reference for hidden-ref git checkpoints that never pollute user branches.
- **K9i-0/ccpocket** (MIT) — https://github.com/K9i-0/ccpocket
  Design reference for offline outgoing queues, idempotency keys, and reconnect catch-up.

## Question answers and agent CLI updates (#179, #178)

- **getpaseo/paseo** (Apache-2.0) — https://github.com/getpaseo/paseo
  `provider.SplitAnswer` is adapted from its `splitMultiSelectAnswer`
  (`packages/server/src/server/agent/providers/question-card-answer.ts`) and
  its tests; the agent CLI update check follows its `release-check.ts` and
  `update-command.ts`.
  Copyright (c) paseo contributors — Apache License 2.0.
