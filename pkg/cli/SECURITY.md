# Security policy

Report suspected vulnerabilities privately through GitHub Security Advisories.
Do not open a public issue containing an exploit, credential, private endpoint,
or secret-bearing argv.

Security fixes are supported on the latest released minor version. A report
should include the affected version, impact, minimal reproduction, and any
known mitigation. Maintainers will acknowledge a report, assess severity,
coordinate a fix and disclosure, and credit reporters who want attribution.

The library never executes a shell, reads environment variables, reads the
working directory, registers process signals, calls `os.Exit`, or starts hidden
goroutines. Applications remain responsible for file, network, configuration,
secret-source, and operating-system policy.
