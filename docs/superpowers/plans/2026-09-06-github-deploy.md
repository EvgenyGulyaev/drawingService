# GitHub deployment implementation plan

**Goal:** Deploy both Go binaries and the game frontend from their existing GitHub repositories.

**Architecture:** Keep the existing drawing service workflow and secrets. Add independent API and frontend workflows using a dedicated game deployment user; serialize activation with a server-side lock, copy the unchanged component into each new release, and atomically switch `/opt/crocodile/current`. Roll back a failed health check. Keep `/var/lib/crocodile/game.db` outside releases.

- [ ] Update `.github/workflows/deploy.yml` to use `go.mod`, test before building, upload a new filename, and preserve the stopped/running state of the old service.
- [ ] Add `.github/workflows/deploy-crocodile.yml` and frontend `.github/workflows/deploy.yml`, triggered by push to main or manual dispatch.
- [ ] Install `deploy/deploy-crocodile.sh` and a sudo rule allowing the deployment user to restart only `crocodile.service`.
- [ ] Check workflows with `actionlint`, script with `shellcheck`, both Go builds and race tests, frontend build. Exercise deployment and rollback on an isolated filesystem before production.
- [ ] Configure game repository secrets, push tested changes, observe Actions to completion, and verify game plus existing sites.
