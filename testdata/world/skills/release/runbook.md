# Release runbook

Rollout order, for an agent executing the release:

1. Deploy to staging, wait for the smoke suite.
2. Promote to canary (one instance), observe one metric window.
3. Promote to full.

Rollback is one command: re-run the previous artifact through the deploy
guard and roll the service back to it. Never roll forward a broken canary.
