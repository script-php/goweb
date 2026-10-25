# 1.0.0-rc.1 checklist

- [x] Public extension API reviewed.
- [x] Authentication helpers exposed without importing internal packages.
- [x] Independent consumer-module compilation enforced.
- [x] Reference consumer covers pages, admin, schemas, media references, jobs,
  rate limits, and template/static overrides.
- [x] Application schema upgrade preserves existing rows.
- [x] Release archive contents and example configuration validated.
- [x] Focused security review recorded.
- [x] Race tests, vet, release metadata, and database integration tests pass.
- [ ] Push the reviewed commit.
- [ ] Create and push the annotated `v1.0.0-rc.1` tag.
- [ ] Verify download and import through `proxy.golang.org` from a clean module.

The final three steps require the committed public tag. Do not reuse the tag if
a release-candidate fix is needed; publish `v1.0.0-rc.2` instead.
