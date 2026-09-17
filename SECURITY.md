# Security Policy

## Supported versions

Fixes are applied to the latest release. Older tags are not patched.

## Reporting a vulnerability

Please do not open a public issue for a security problem.

Report it privately using GitHub's
[private vulnerability reporting](https://github.com/mrichman/hargo/security/advisories/new),
or by email to mark@markrichman.com.

Include the version or commit, the platform, and the steps needed to reproduce
the issue. A proof-of-concept `.har` file is the most useful thing you can send,
since most of the attack surface is HAR parsing.

You can expect an acknowledgement within a week.

## Scope

hargo parses untrusted `.har` files and replays the HTTP requests they describe.
The areas most likely to matter:

- **Parsing.** Malformed or hostile HAR input should produce an error, never a
  panic, an unbounded allocation, or a hang. The parsers are fuzzed; see
  `fuzz_test.go` and `make fuzz`.
- **`hargo fetch` writing files.** Output names are derived from the URL path via
  `path.Base`, which strips `..` traversal, and colliding names get a numeric
  suffix rather than overwriting. A path escaping the output directory is a
  vulnerability.
- **Request replay.** `hargo run`, `fetch`, and `load` send the requests recorded
  in the HAR, including its cookies and credentials. This is the intended
  behaviour, so treat a `.har` file as a secret: it frequently contains session
  cookies and `Authorization` headers.

Note that `--insecure-skip-verify` disables TLS certificate verification by
design. Using it is not a vulnerability in hargo.
