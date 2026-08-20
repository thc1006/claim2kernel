# Contributing

Changes that affect contract or admission semantics must include:

- a threat-model note;
- positive and negative tests;
- a stable rejection code for new runtime failures;
- schema and documentation updates;
- no weakening of fail-closed behavior;
- no use of the independent test split for fitting or bound selection;
- retained raw hardware evidence for performance claims.

Run `make validate-release` before submitting. Mojo/GPU changes require the
manual self-hosted workflow and attached raw build/measurement artifacts.
