# Publishing and consuming GoWeb

GoWeb supports both repository clones and applications that import the public
platform package. Releases use semantic versions and tags such as `v1.0.0-rc.1`.

## Canonical module resolution

The canonical module path is intentionally `script-php/goweb`. Unlike a path
beginning with a DNS name such as `github.com`, this cannot be discovered by
Go's direct VCS resolver. Public package use therefore requires a module proxy
that implements the Go proxy protocol for this exact module path.

Set the proxy for consuming builds:

```sh
go env -w GOPROXY=https://modules.example.com,https://proxy.golang.org,direct
go get script-php/goweb@v1.0.0-rc.1
```

The proxy must publish these endpoints, with escaped versions as defined by the
Go module proxy protocol:

```text
/script-php/goweb/@v/list
/script-php/goweb/@v/v1.0.0-rc.1.info
/script-php/goweb/@v/v1.0.0-rc.1.mod
/script-php/goweb/@v/v1.0.0-rc.1.zip
```

If releases are private or absent from the public checksum database, configure
the consumer with `GONOSUMDB=script-php/goweb`. Never put proxy credentials in
`go.mod`; supply them through the build environment or credential helper.

Until a proxy is connected, local development uses:

```go
require script-php/goweb v0.0.0
replace script-php/goweb => ../goweb
```

The source repository is `https://github.com/script-php/goweb`. Because the
canonical module path intentionally differs from that repository URL, consumers
need the documented module proxy mapping; the module path itself does not
change.

## Prepare a release

1. Choose the semantic version based on the public `platform` API.
2. Update `version.txt` and add the matching `## x.y.z` heading to
   `CHANGELOG.md`.
3. Run `make release-check` and the database integration matrix locally when
   possible.
4. Build the same ZIP files that GitHub will publish:

   ```sh
   make release
   ```

   This creates Linux AMD64 and ARM64 packages plus `SHA256SUMS` under `dist/`.
   Each ZIP contains a clean deployable directory with the binary, example
   `config.json`, empty `uploads/`, documentation, and dependency manifest. It
   never copies the live `release/config.json`, uploads, backups, or
   `tables.hash`. Use `RELEASE_ARCHES=amd64` to build only one architecture.
5. Commit the complete release and ensure the working tree is clean.
6. Create an annotated tag on that commit:

   ```sh
   git tag -a v1.0.0-rc.1 -m "GoWeb v1.0.0-rc.1"
   git push origin main v1.0.0-rc.1
   ```

Do not tag a dirty or older commit. The release workflow rejects a tag that
does not exactly match `version.txt` or `CHANGELOG.md`.

When the tag reaches GitHub, `.github/workflows/release.yml` reruns tests, vet,
vulnerability scanning, and archive validation. It runs the same `make release`
target to create static Linux AMD64 and ARM64 ZIP files, records the Go module
graph in `DEPENDENCIES.txt`, produces
SHA-256 checksums, uploads the workflow artifact, and creates the GitHub release.

## CI guarantees

The normal workflow tests Go 1.24 and the current stable Go release. Its
database matrix applies the actual embedded schemas and runs integration tests
against MySQL 8.0, MySQL 8.4, MariaDB 10.6, and MariaDB 11.4. The matrix performs
a real backup and restore, so it uses isolated disposable service containers.

Release metadata is validated on every build. Branch and pull-request builds
validate the version, changelog, and module path; exact tag-to-version matching
is enforced only for tag builds or when `RELEASE_TAG` is explicitly supplied.

## Upgrade an importing application

Review `CHANGELOG.md` and `UPGRADING.md`, then update and test on a branch:

```sh
go get script-php/goweb@v1.0.0-rc.1
go mod tidy
go test ./...
go build ./...
```

Before deploying, run the application's schema plan against staging, review the
SQL, create a verified backup, and exercise authentication and admin workflows.
Commit both `go.mod` and `go.sum` after the upgrade succeeds.
