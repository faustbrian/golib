<?php declare(strict_types=1);

use Cline\Temporal\Period\Bounds;
use Cline\Temporal\Time\Duration;
use Cline\Temporal\Time\Interval;
use Cline\Temporal\Time\IntervalFormat;
use Cline\Temporal\Time\RoundingMode;
use Cline\Temporal\Time\Time;
use Cline\Temporal\Time\Unit;

if (!function_exists('throw_if')) {
    function throw_if(bool $condition, string $exception, mixed ...$parameters): void
    {
        if ($condition) {
            throw new $exception(...$parameters);
        }
    }
}

if (!function_exists('throw_unless')) {
    function throw_unless(bool $condition, string $exception, mixed ...$parameters): void
    {
        throw_if(!$condition, $exception, ...$parameters);
    }
}

$source = $argv[1] ?? null;
if (!is_string($source) || !is_dir($source.'/src')) {
    fwrite(STDERR, "usage: php compat/php/generate.php /path/to/php-temporal\n");
    exit(2);
}

spl_autoload_register(static function (string $class) use ($source): void {
    $prefix = 'Cline\\Temporal\\';
    if (!str_starts_with($class, $prefix)) {
        return;
    }
    $relative = str_replace('\\', '/', substr($class, strlen($prefix)));
    require $source.'/src/'.$relative.'.php';
});

/** @return list<string> */
function public_api_inventory(string $source): array
{
    $inventory = [];
    $iterator = new RecursiveIteratorIterator(
        new RecursiveDirectoryIterator($source.'/src', FilesystemIterator::SKIP_DOTS),
    );

    foreach ($iterator as $file) {
        if (!$file instanceof SplFileInfo || $file->getExtension() !== 'php') {
            continue;
        }
        $relative = str_replace('\\', '/', substr($file->getPathname(), strlen($source.'/src/')));
        if (str_contains($relative, '/Chart/') || str_ends_with($relative, 'Test.php')) {
            continue;
        }

        $contents = file_get_contents($file->getPathname());
        if (!is_string($contents)) {
            throw new RuntimeException('unable to read PHP source: '.$relative);
        }
        if (!preg_match('/namespace\s+([^;]+);/', $contents, $namespaceMatch) ||
            !preg_match(
                '/^\s*(?:(?:final|abstract|readonly)\s+)*(?:class|interface|enum|trait)\s+([A-Za-z_][A-Za-z0-9_]*)/m',
                $contents,
                $typeMatch,
            )) {
            continue;
        }
        $type = trim($namespaceMatch[1]).'\\'.$typeMatch[1];
        $inventory[] = $type;

        preg_match_all(
            '/public\s+(?:static\s+)?function\s+&?\s*([A-Za-z_][A-Za-z0-9_]*)\s*\(/',
            $contents,
            $methodMatches,
        );
        foreach ($methodMatches[1] as $method) {
            $inventory[] = $type.'::'.$method.'()';
        }

        preg_match_all('/^\s*case\s+([A-Za-z_][A-Za-z0-9_]*)/m', $contents, $caseMatches);
        foreach ($caseMatches[1] as $case) {
            $inventory[] = $type.'::'.$case;
        }

        preg_match_all('/public\s+const\s+(?:[A-Za-z_|?]+\s+)?([A-Za-z_][A-Za-z0-9_]*)/', $contents, $constantMatches);
        foreach ($constantMatches[1] as $constant) {
            $inventory[] = $type.'::'.$constant;
        }

        preg_match_all(
            '/public\s+(?:readonly\s+)?(?:[?\\\\A-Za-z_|&][?\\\\A-Za-z0-9_|&]*\s+)?\$([A-Za-z_][A-Za-z0-9_]*)/',
            $contents,
            $propertyMatches,
        );
        foreach ($propertyMatches[1] as $property) {
            $inventory[] = $type.'::$'.$property;
        }
    }

    $inventory = array_values(array_unique($inventory));
    sort($inventory, SORT_STRING);

    return $inventory;
}

/**
 * @param list<string> $inventory
 * @return list<array{symbol: string, status: string, contract: string, go_evidence: string, migration: string}>
 */
function behavior_coverage(array $inventory): array
{
    $profiles = [
        'Cline\\Temporal\\Period\\Bounds' => ['bound notation and inclusion', 'go-test: TestPinnedPHPCompatibilityFixture; TestBracketSemanticsAreExact', 'docs/migration.md#bounds'],
        'Cline\\Temporal\\Period\\DatePoint' => ['instant and civil-date construction and relations', 'go-test: TestAllenRelationsAcrossAllBounds; TestDateFactoriesUseCalendarUnits', 'docs/migration.md#date-and-time-values'],
        'Cline\\Temporal\\Period\\Duration' => ['fixed versus calendar duration conversion', 'go-test: TestFixedISODurationParsing; TestSnapSupportsEveryCivilUnit', 'docs/migration.md#fixed-versus-calendar-duration'],
        'Cline\\Temporal\\Period\\Period' => ['period construction, relations, algebra, and iteration', 'go-test: TestAllenRelationsAcrossAllBounds; TestInstantSetAlgebraProperties; FuzzInstantSplitting', 'docs/migration.md#periods-and-sets'],
        'Cline\\Temporal\\Period\\Sequence' => ['normalized immutable period collections', 'go-test: TestSetIterationSearchTransformAndReduction; TestInstantSetAlgebraProperties', 'docs/migration.md#periods-and-sets'],
        'Cline\\Temporal\\Time\\Bound' => ['endpoint side selection', 'go-test: TestPinnedPHPCompatibilityFixture', 'docs/compatibility.md#time-namespace'],
        'Cline\\Temporal\\Time\\Duration' => ['checked fixed-duration arithmetic', 'go-test: TestDurationCheckedSumNegateAndAbsolute; TestDurationMultiplyDivideAndRemainder', 'docs/migration.md#fixed-versus-calendar-duration'],
        'Cline\\Temporal\\Time\\DurationFormat' => ['fixed-duration encoding', 'go-test: TestFixedISODurationFormatRoundTrips', 'docs/compatibility.md#time-namespace'],
        'Cline\\Temporal\\Time\\Interval' => ['circular daily interval algebra and iteration', 'go-test: TestCircularDailyAlgebraAcrossEveryHourAndBounds; FuzzDailyIntervalIteration', 'docs/migration.md#daily-time'],
        'Cline\\Temporal\\Time\\IntervalFormat' => ['daily interval encoding', 'go-test: TestDailyBoundedNotationRoundTripsEveryKindAndBounds', 'docs/compatibility.md#time-namespace'],
        'Cline\\Temporal\\Time\\IntervalSet' => ['normalized circular interval collections', 'go-test: TestCircularSetAlgebraProperties; TestDailySetCopiesOutputsAndEnforcesExpansionLimits', 'docs/migration.md#daily-time'],
        'Cline\\Temporal\\Time\\IntervalType' => ['explicit ordinary, circular, collapsed, and full-day kinds', 'go-test: TestIntervalKindsAreExplicit', 'docs/compatibility.md#time-namespace'],
        'Cline\\Temporal\\Time\\RoundingMode' => ['explicit temporal rounding direction', 'go-test: TestTimeRoundUsesExplicitDirectionAndDailyBoundary', 'docs/compatibility.md#time-namespace'],
        'Cline\\Temporal\\Time\\Time' => ['validated local time values and circular arithmetic', 'go-test: TestTimeConstructionAndNamedValues; TestShiftRequiresExplicitWrappingPolicy', 'docs/migration.md#daily-time'],
        'Cline\\Temporal\\Time\\TimeFormat' => ['strict local-time encoding', 'go-test: TestStrictTimeParsingAndRoundTrip', 'docs/compatibility.md#time-namespace'],
        'Cline\\Temporal\\Time\\TimeFormatLength' => ['locale-specific presentation length', 'go-divergence: locale presentation is outside temporal core', 'docs/compatibility.md#time-namespace'],
        'Cline\\Temporal\\Time\\Unit' => ['fixed-duration units and arithmetic', 'go-test: TestDurationConstructionAndComparison; TestDurationMultiplyDivideAndRemainder', 'docs/compatibility.md#time-namespace'],
    ];
    $errorTypes = [
        'Cline\\Temporal\\Period\\InaccessibleInterval',
        'Cline\\Temporal\\Period\\IntervalError',
        'Cline\\Temporal\\Period\\InvalidInterval',
        'Cline\\Temporal\\Period\\UnprocessableInterval',
        'Cline\\Temporal\\Time\\InvalidDuration',
        'Cline\\Temporal\\Time\\InvalidInterval',
        'Cline\\Temporal\\Time\\InvalidIntervalSetOffset',
        'Cline\\Temporal\\Time\\InvalidTime',
        'Cline\\Temporal\\Time\\InvalidTimezone',
        'Cline\\Temporal\\Time\\TimeException',
    ];
    foreach ($errorTypes as $type) {
        $profiles[$type] = ['typed failure classification', 'go-test: typed errors asserted with errors.Is and errors.As throughout package tests', 'docs/migration.md#error-handling'];
    }
    $profiles['Cline\\Temporal\\Period\\MissingFeature'] = ['PHP feature marker without a core Go value', 'go-divergence: unsupported features are documented capability gaps', 'docs/compatibility.md#compatibility-policy'];
    $profiles['Cline\\Temporal\\Period\\PeriodTestCase'] = ['PHP test-framework helper', 'go-divergence: temporaltest provides Go-native assertions', 'docs/compatibility.md#period-namespace'];
    $profiles['Cline\\Temporal\\Time\\UnableToFormatLocaleTime'] = ['locale presentation failure', 'go-divergence: locale presentation is outside temporal core', 'docs/compatibility.md#time-namespace'];
    $profiles['Cline\\Temporal\\Time\\UnsupportedLocaleFormatting'] = ['locale presentation failure', 'go-divergence: locale presentation is outside temporal core', 'docs/compatibility.md#time-namespace'];

    $divergent = array_fill_keys([
        'Cline\\Temporal\\Period\\Bounds::equalsEnd()',
        'Cline\\Temporal\\Period\\Bounds::equalsStart()',
        'Cline\\Temporal\\Period\\DatePoint::$date',
        'Cline\\Temporal\\Period\\DatePoint::__set_state()',
        'Cline\\Temporal\\Period\\DatePoint::fromDate()',
        'Cline\\Temporal\\Period\\DatePoint::fromDateString()',
        'Cline\\Temporal\\Period\\DatePoint::fromFormat()',
        'Cline\\Temporal\\Period\\DatePoint::fromTimestamp()',
        'Cline\\Temporal\\Period\\DatePoint::hour()',
        'Cline\\Temporal\\Period\\DatePoint::minute()',
        'Cline\\Temporal\\Period\\DatePoint::second()',
        'Cline\\Temporal\\Period\\Period::__set_state()',
        'Cline\\Temporal\\Period\\Period::dateInterval()',
        'Cline\\Temporal\\Period\\Period::dateIntervalDiff()',
        'Cline\\Temporal\\Period\\Period::fromDate()',
        'Cline\\Temporal\\Period\\Period::fromRange()',
        'Cline\\Temporal\\Period\\Period::fromTimestamp()',
        'Cline\\Temporal\\Period\\Period::gap()',
        'Cline\\Temporal\\Period\\Period::intersect()',
        'Cline\\Temporal\\Period\\Period::rangeBackwards()',
        'Cline\\Temporal\\Period\\Period::rangeForward()',
        'Cline\\Temporal\\Period\\Period::snapToDay()',
        'Cline\\Temporal\\Period\\Period::snapToHour()',
        'Cline\\Temporal\\Period\\Period::snapToIsoWeek()',
        'Cline\\Temporal\\Period\\Period::snapToIsoYear()',
        'Cline\\Temporal\\Period\\Period::snapToMinute()',
        'Cline\\Temporal\\Period\\Period::snapToMonth()',
        'Cline\\Temporal\\Period\\Period::snapToQuarter()',
        'Cline\\Temporal\\Period\\Period::snapToSecond()',
        'Cline\\Temporal\\Period\\Period::snapToSemester()',
        'Cline\\Temporal\\Period\\Period::snapToYear()',
        'Cline\\Temporal\\Period\\Period::timeDurationDiff()',
        'Cline\\Temporal\\Period\\Sequence::clear()',
        'Cline\\Temporal\\Period\\Sequence::get()',
        'Cline\\Temporal\\Period\\Sequence::indexOf()',
        'Cline\\Temporal\\Period\\Sequence::insert()',
        'Cline\\Temporal\\Period\\Sequence::offsetExists()',
        'Cline\\Temporal\\Period\\Sequence::offsetGet()',
        'Cline\\Temporal\\Period\\Sequence::offsetSet()',
        'Cline\\Temporal\\Period\\Sequence::offsetUnset()',
        'Cline\\Temporal\\Period\\Sequence::push()',
        'Cline\\Temporal\\Period\\Sequence::remove()',
        'Cline\\Temporal\\Period\\Sequence::set()',
        'Cline\\Temporal\\Period\\Sequence::some()',
        'Cline\\Temporal\\Period\\Sequence::sort()',
        'Cline\\Temporal\\Period\\Sequence::unshift()',
        'Cline\\Temporal\\Time\\Duration::__serialize()',
        'Cline\\Temporal\\Time\\Duration::__unserialize()',
        'Cline\\Temporal\\Time\\Duration::format()',
        'Cline\\Temporal\\Time\\Duration::fromDateInterval()',
        'Cline\\Temporal\\Time\\Duration::fromFormat()',
        'Cline\\Temporal\\Time\\Duration::jsonSerialize()',
        'Cline\\Temporal\\Time\\Duration::max()',
        'Cline\\Temporal\\Time\\Duration::maxOf()',
        'Cline\\Temporal\\Time\\Duration::min()',
        'Cline\\Temporal\\Time\\Duration::minOf()',
        'Cline\\Temporal\\Time\\Duration::toDateInterval()',
        'Cline\\Temporal\\Time\\Duration::total()',
        'Cline\\Temporal\\Time\\Interval::__serialize()',
        'Cline\\Temporal\\Time\\Interval::__unserialize()',
        'Cline\\Temporal\\Time\\Interval::roundTo()',
        'Cline\\Temporal\\Time\\Interval::shiftBound()',
        'Cline\\Temporal\\Time\\Interval::splitAt()',
        'Cline\\Temporal\\Time\\Interval::toNative()',
        'Cline\\Temporal\\Time\\IntervalSet::__serialize()',
        'Cline\\Temporal\\Time\\IntervalSet::__unserialize()',
        'Cline\\Temporal\\Time\\IntervalSet::allNative()',
        'Cline\\Temporal\\Time\\IntervalSet::any()',
        'Cline\\Temporal\\Time\\IntervalSet::each()',
        'Cline\\Temporal\\Time\\IntervalSet::every()',
        'Cline\\Temporal\\Time\\IntervalSet::filter()',
        'Cline\\Temporal\\Time\\IntervalSet::first()',
        'Cline\\Temporal\\Time\\IntervalSet::firstMatching()',
        'Cline\\Temporal\\Time\\IntervalSet::get()',
        'Cline\\Temporal\\Time\\IntervalSet::getIterator()',
        'Cline\\Temporal\\Time\\IntervalSet::has()',
        'Cline\\Temporal\\Time\\IntervalSet::indexOf()',
        'Cline\\Temporal\\Time\\IntervalSet::last()',
        'Cline\\Temporal\\Time\\IntervalSet::lastIndexOf()',
        'Cline\\Temporal\\Time\\IntervalSet::lastMatching()',
        'Cline\\Temporal\\Time\\IntervalSet::map()',
        'Cline\\Temporal\\Time\\IntervalSet::nth()',
        'Cline\\Temporal\\Time\\IntervalSet::push()',
        'Cline\\Temporal\\Time\\IntervalSet::reduce()',
        'Cline\\Temporal\\Time\\IntervalSet::remove()',
        'Cline\\Temporal\\Time\\IntervalSet::replace()',
        'Cline\\Temporal\\Time\\IntervalSet::sortedUsing()',
        'Cline\\Temporal\\Time\\IntervalSet::transform()',
        'Cline\\Temporal\\Time\\IntervalSet::unshift()',
        'Cline\\Temporal\\Time\\Time::__serialize()',
        'Cline\\Temporal\\Time\\Time::__unserialize()',
        'Cline\\Temporal\\Time\\Time::applyTo()',
        'Cline\\Temporal\\Time\\Time::fromDate()',
        'Cline\\Temporal\\Time\\Time::maxOf()',
        'Cline\\Temporal\\Time\\Time::minOf()',
        'Cline\\Temporal\\Time\\Time::now()',
        'Cline\\Temporal\\Time\\Time::roundTo()',
        'Cline\\Temporal\\Time\\Time::shift()',
        'Cline\\Temporal\\Time\\Time::toLocaleString()',
        'Cline\\Temporal\\Time\\Time::with()',
    ], true);

    $coverage = [];
    foreach ($inventory as $symbol) {
        $type = explode('::', $symbol, 2)[0];
        if (!isset($profiles[$type])) {
            throw new RuntimeException('unclassified public PHP type: '.$type);
        }
        [$contract, $evidence, $migration] = $profiles[$type];
        $typeDiverges = in_array($type, [
            'Cline\\Temporal\\Period\\Duration',
            'Cline\\Temporal\\Period\\MissingFeature',
            'Cline\\Temporal\\Period\\PeriodTestCase',
            'Cline\\Temporal\\Time\\DurationFormat',
            'Cline\\Temporal\\Time\\TimeFormatLength',
            'Cline\\Temporal\\Time\\UnableToFormatLocaleTime',
            'Cline\\Temporal\\Time\\UnsupportedLocaleFormatting',
        ], true);
        $status = $typeDiverges || isset($divergent[$symbol]) ? 'diverges' : 'supported';
        if ($status === 'diverges' && !str_starts_with($evidence, 'go-divergence:')) {
            $evidence = 'go-divergence: explicit Go replacement or omission; replacement evidence: '.substr($evidence, strlen('go-test: '));
        }
        $coverage[] = [
            'symbol' => $symbol,
            'status' => $status,
            'contract' => $contract,
            'go_evidence' => $evidence,
            'migration' => $migration,
        ];
    }

    return $coverage;
}

$bounds = [];
foreach (Bounds::cases() as $value) {
    $bounds[] = [
        'php_case' => $value->name,
        'includes_start' => $value->isStartIncluded(),
        'includes_end' => $value->isEndIncluded(),
        'iso_80000' => $value->buildIso80000('08:00', '17:00'),
        'bourbaki' => $value->buildBourbaki('08:00', '17:00'),
        'include_start' => $value->includeStart()->name,
        'include_end' => $value->includeEnd()->name,
        'exclude_start' => $value->excludeStart()->name,
        'exclude_end' => $value->excludeEnd()->name,
    ];
}

$timeValues = [
    'midnight' => Time::midnight(),
    'noon' => Time::noon(),
    'fractional' => Time::at(12, 34, 56, 123_456),
    'php_end_of_day' => Time::endOfDay(),
];
$times = [];
foreach ($timeValues as $name => $value) {
    $times[] = [
        'name' => $name,
        'iso' => $value->format(),
        'microseconds' => $value->toOffset(Unit::Microsecond),
    ];
}

$intervalValues = [
    'ordinary' => Interval::between(Time::at(8), Time::at(17)),
    'circular' => Interval::between(Time::at(22), Time::at(2)),
    'collapsed' => Interval::collapsed(Time::at(8)),
    'full_day' => Interval::fullDay(),
];
$intervals = [];
foreach ($intervalValues as $name => $value) {
    $intervals[] = [
        'name' => $name,
        'iso_80000' => $value->format(IntervalFormat::Iso80000),
        'bourbaki' => $value->format(IntervalFormat::Bourbaki),
        'start_end' => $value->format(IntervalFormat::Iso8601StartEnd),
        'duration' => $value->duration->format(),
        'type' => $value->type->name,
    ];
}

$left = Interval::between(Time::at(10), Time::at(12));
$overlap = Interval::between(Time::at(11), Time::at(13));
$abut = Interval::between(Time::at(12), Time::at(14));
$gap = Interval::between(Time::at(14), Time::at(16));
$duration = Duration::of(hours: 1, minutes: 30, milliseconds: 500);
$negativeDuration = $duration->negated();
$time = Time::at(23, 30);
$intersection = $left->intersect($overlap);
$betweenGap = $left->gap($gap);

$publicAPI = public_api_inventory($source);
$fixture = [
    'schema' => 'php-temporal-compat/v1',
    'source' => [
        'repository' => 'github.com/faustbrian/golib/pkg/temporal',
        'commit' => '469603239dbe700739c29b4c532a90382b6cbedf',
        'namespace' => 'Cline\\Temporal',
    ],
    'public_api' => $publicAPI,
    'behavior_coverage' => behavior_coverage($publicAPI),
    'bounds' => $bounds,
    'times' => $times,
    'intervals' => $intervals,
    'duration_operations' => [
        'base' => $duration->format(),
        'negated' => $negativeDuration->format(),
        'absolute' => $negativeDuration->abs()->format(),
        'sum' => $duration->sum(Duration::of(minutes: 30))->format(),
        'multiplied' => $duration->multipliedBy(2)->format(),
        'divided' => $duration->dividedBy(2)->format(),
        'rounded_hour' => $duration->roundTo(Unit::Hour, RoundingMode::Nearest)->format(),
        'compare_longer' => $duration->isLongerThan(Duration::of(hours: 1)),
    ],
    'time_operations' => [
        'shift_wrap' => $time->shift(Duration::of(hours: 2))->format(),
        'round_hour' => $time->roundTo(Unit::Hour, RoundingMode::Nearest)->format(),
        'difference' => Time::at(22)->diff(Time::at(2))->format(),
        'distance' => Time::at(22)->distance(Time::at(2))->format(),
        'clamp' => Time::at(7)->clamp(Time::at(8), Time::at(17))->format(),
    ],
    'interval_operations' => [
        'intersection' => $intersection?->format(IntervalFormat::Iso80000),
        'gap' => $betweenGap?->format(IntervalFormat::Iso80000),
        'union' => $left->union($overlap)->allFormatted(IntervalFormat::Iso80000),
        'difference' => $left->difference($overlap)->allFormatted(IntervalFormat::Iso80000),
        'complement' => $left->complement()->format(IntervalFormat::Iso80000),
        'split' => $left->splitBy(Duration::of(minutes: 30))->allFormatted(IntervalFormat::Iso80000),
        'steps' => array_map(
            static fn (Time $step): string => $step->format(),
            iterator_to_array($left->steps(Duration::of(minutes: 30))),
        ),
    ],
    'predicates' => [
        'includes_inside' => $left->includes(Time::at(11)),
        'includes_excluded_end' => $left->includes(Time::at(12)),
        'overlaps' => $left->overlaps($overlap),
        'does_not_overlap_abutting' => $left->overlaps($abut),
        'abuts' => $left->abuts($abut),
        'does_not_abut_gap' => $left->abuts($gap),
    ],
];

echo json_encode($fixture, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR)."\n";
