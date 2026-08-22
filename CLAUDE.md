# CLAUDE.md

Development guidance for this repository lives in [`AGENTS.md`](AGENTS.md). Read
it before making changes.

It covers the constraints that matter most here:

- MockGatehub is consumed downstream as a container image, so every route and
  response field is a published contract.
  `features/consumer_contract.feature` pins what consumers depend on.
- The application API and the admin surface are served on **two separate
  ports**, and routes must be registered on the correct one.
- Storage has two backends; a change to one must be made to both.
- `make test` runs lint, unit tests and the BDD end-to-end suite. End-to-end
  scenarios must assert observable behaviour, not just HTTP status codes.
