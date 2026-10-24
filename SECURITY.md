# Security policy

## Supported versions

brownout is a library on the v0 release line. Security fixes go into the
latest release (currently v0.1.x) and `master`; upgrade to the newest tag to
pick them up. Older v0 releases are not patched.

## Reporting a vulnerability

Please don't open a public issue. Report it privately through GitHub's
private vulnerability reporting:

<https://github.com/rossedman/brownout/security/advisories/new>

Include what you found, how to reproduce it (a failing test or a request
against one of the `examples/` programs is ideal) and the version or commit
you tested.

What to expect:

- an acknowledgement within 3 business days;
- a fix, or a plan with a timeline, within 30 days;
- credit in the published advisory, unless you'd rather stay anonymous.

## Verifying releases

brownout is installed with `go get`, which checks every module against the Go
checksum database, so most users need nothing more. Each GitHub release from
v0.1.4 on also carries a source archive and a `checksums.txt` signed keylessly
with [cosign](https://docs.sigstore.dev/) by the release workflow, plus a
build provenance attestation. To verify a downloaded archive:

```sh
cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/rossedman/brownout/.github/workflows/release.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum --ignore-missing -c checksums.txt
gh attestation verify brownout_*_source.tar.gz --repo rossedman/brownout
```

## Scope

`AdminHandler` performs no authentication by design; exposing it without your
own auth is a deployment issue, not a vulnerability in brownout (see
`examples/admin-auth`). Bugs that let a request bypass a rule, crash the
middleware, or let a rules file or admin request corrupt the active rule set
are in scope.
