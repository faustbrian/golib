# Security

Report vulnerabilities through GitHub private vulnerability reporting for the
repository. Do not include live credentials or secret prompt values in an
issue, test fixture, terminal capture, or proof of concept.

There is no supported release while the module remains in initial development.
After the first release, the latest minor line will receive security fixes and
the support window will be recorded here. Security advisories will describe
affected versions, safe upgrade versions, and whether secret disclosure or
terminal-state restoration is involved.

The package redacts its secret wrapper representations and safe errors. It
cannot control copies made after `Reveal`, application logging, clipboard
software, terminal emulators, swap, crash dumps, or Go string memory. See
`docs/security.md` and `docs/secrets.md` before handling credentials.
