#!/bin/sh
set -eu

if ! command -v php >/dev/null 2>&1; then
	printf '%s\n' 'php is required for interoperability verification' >&2
	exit 1
fi

go test -run 'TestAlgorithmAdaptersAndPHPFixtures|TestMaintainedImplementationVectors' ./

# shellcheck disable=SC2016 # PHP variables must not expand in the shell.
php -r '
$password = "synthetic password";
$hashes = [
    "\$2y\$10\$ABk0ypUBDSb78zn66THffuHDCkhvUWaMk2g..sQiEEfh1RemSi6vm",
    "\$argon2id\$v=19\$m=65536,t=2,p=1\$SBj4Q9N+Krb5qUX9O00GHg\$r0xVBSfxyYkNbAcWCI8kZHSz5Z3U/vV9bx8o7aYjxbc",
];
foreach ($hashes as $hash) {
    if (!password_verify($password, $hash)) {
        fwrite(STDERR, "PHP fixture verification failed\n");
        exit(1);
    }
}
'

php_output=$(mktemp "${TMPDIR:-/tmp}/password-php-interop.XXXXXX")
go_output=$(mktemp "${TMPDIR:-/tmp}/password-go-interop.XXXXXX")
trap 'rm -f "$php_output" "$go_output"' EXIT HUP INT TERM

# shellcheck disable=SC2016 # PHP variables must not expand in the shell.
php -r '
$password = "synthetic password";
echo password_hash($password, PASSWORD_ARGON2ID, [
    "memory_cost" => 65536,
    "time_cost" => 2,
    "threads" => 1,
]), PHP_EOL;
echo password_hash($password, PASSWORD_BCRYPT, ["cost" => 10]), PHP_EOL;
' >"$php_output"
go run ./scripts/verify-interoperability.go "$php_output"

go run ./scripts/generate-interoperability.go >"$go_output"
argon=$(sed -n '1p' "$go_output")
bcrypt=$(sed -n '2p' "$go_output")
# shellcheck disable=SC2016 # PHP variables must not expand in the shell.
php -r '
$password = "synthetic password";
foreach (array_slice($argv, 1) as $hash) {
    if (!password_verify($password, $hash)) {
        fwrite(STDERR, "Go hash verification in PHP failed\n");
        exit(1);
    }
}
' "$argon" "$bcrypt"
