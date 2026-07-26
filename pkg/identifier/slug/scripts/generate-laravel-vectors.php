<?php

declare(strict_types=1);

/**
 * Generate differential vectors through the frozen Laravel runtime.
 *
 * Usage:
 * php scripts/generate-laravel-vectors.php \
 *   --autoload=/path/to/location/vendor/autoload.php \
 *   --output=testdata/laravel_english.json
 */

$options = getopt('', ['autoload:', 'output:']);
$autoload = $options['autoload'] ?? null;
$output = $options['output'] ?? null;

if (!is_string($autoload) || !is_file($autoload)) {
    fwrite(STDERR, "a valid --autoload file is required\n");
    exit(1);
}
if (!is_string($output) || trim($output) === '') {
    fwrite(STDERR, "--output is required\n");
    exit(1);
}

require $autoload;

$replacementMap = \voku\helper\ASCII::charsArrayWithSingleLanguageValues(
    false,
    false,
);
$sources = [
    '',
    'Carrier@example.com',
    "mixed\0control\tid",
    str_repeat('å', 250).' suffix',
];
foreach (array_keys($replacementMap) as $character) {
    $sources[] = 'before '.$character.' after';
}
$sources = array_values(array_unique($sources));

$vectors = [];
foreach ($sources as $source) {
    $vectors[] = [
        'source' => $source,
        'expected' => \Illuminate\Support\Str::slug(
            mb_substr($source, 0, 250),
        ),
    ];
}

$json = json_encode(
    $vectors,
    JSON_PRETTY_PRINT
        | JSON_UNESCAPED_SLASHES
        | JSON_UNESCAPED_UNICODE
        | JSON_THROW_ON_ERROR,
);
if (file_put_contents($output, $json."\n") === false) {
    fwrite(STDERR, "unable to write {$output}\n");
    exit(1);
}
