# Public Alpha Release Validation

Date: 2026-10-03

## Published releases

| Version | Commit | Status | Release |
|---|---|---|---|
| `v0.1.0-alpha.1` | `0298138` | Published, superseded | [GitHub release](https://github.com/nmdra/docmcp/releases/tag/v0.1.0-alpha.1) |
| `v0.1.0-alpha.2` | `f78310e` | Published, current | [GitHub release](https://github.com/nmdra/docmcp/releases/tag/v0.1.0-alpha.2) |

Alpha.1 used the wrong Go module path, `github.com/docmcp/docmcp`. Go could not
install it from the repository at `github.com/nmdra/docmcp`. That release remains
published. Alpha.2 changes the module path and imports to `github.com/nmdra/docmcp`.

## Alpha.2 assets

The approved first-alpha profile publishes one Linux amd64 archive and its
checksum file. The archive is `docmcp_Linux_x86_64.tar.gz`, 5,451,015 bytes. Its
SHA-256 is:

```text
9ae4e6b9e4b7466615b2acc1e7730753732045b069d2ac17e600248b862e58e5
```

The release is a GitHub pre-release. It has no artifact signature or SBOM. The
standard multi-platform release profile remains unverified because this build
host lacks an AArch64 cgo cross-compiler.

## Verification

The following checks passed for alpha.2:

- `make ci` passed, including unit, race, integration, and local provider tests.
- `go vet ./...` and `golangci-lint run` passed.
- Both `goreleaser check` and
  `goreleaser check --config .goreleaser-alpha.yaml` passed.
- The Linux amd64 snapshot built and its checksum verified.
- Downloading the published archive and running `sha256sum -c checksums.txt`
  passed. The extracted binary reported `0.1.0-alpha.2` and its help command ran.
- `go install github.com/nmdra/docmcp/cmd/docmcp@v0.1.0-alpha.2` passed.
- The README command `go install github.com/nmdra/docmcp/cmd/docmcp@latest` passed
  after alpha.2 became available through the Go module proxy.

## Scope and remaining gates

Alpha.2 targets Linux amd64 only. The full signed multi-platform release gate,
remaining UX and diagnostics work, and broader retrieval validation remain open.
These alpha results do not establish production readiness.
