# Release Process

1. Ensure `CHANGELOG.md` is up to date for the release.
2. Run local checks:
   - `make test`
   - `make race`
   - `make lint`
   - `make bench`
3. Tag the release using semantic versioning (e.g., `v1.2.3`).
4. Push tags and create a GitHub release with the changelog notes.
