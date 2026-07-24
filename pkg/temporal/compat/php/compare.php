<?php declare(strict_types=1);

const MAX_FIXTURE_BYTES = 1_048_576;

if ($argc !== 3) {
    fwrite(STDERR, "usage: php compat/php/compare.php generated.json expected.json\n");
    exit(2);
}

function fixture(string $path): mixed
{
    if (!is_file($path)) {
        throw new RuntimeException("fixture is not a file: {$path}");
    }

    $size = filesize($path);
    if (!is_int($size) || $size > MAX_FIXTURE_BYTES) {
        throw new RuntimeException("fixture exceeds byte limit: {$path}");
    }

    $contents = file_get_contents($path);
    if (!is_string($contents)) {
        throw new RuntimeException("fixture cannot be read: {$path}");
    }

    return json_decode($contents, true, 32, JSON_THROW_ON_ERROR);
}

try {
    $generated = fixture($argv[1]);
    $expected = fixture($argv[2]);
} catch (Throwable $error) {
    fwrite(STDERR, $error->getMessage()."\n");
    exit(2);
}

if ($generated != $expected) {
    fwrite(STDERR, "generated PHP compatibility fixture differs from expected\n");
    exit(1);
}
