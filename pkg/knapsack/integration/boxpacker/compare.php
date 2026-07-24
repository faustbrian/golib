<?php

declare(strict_types=1);

use DVDoug\BoxPacker\Box;
use DVDoug\BoxPacker\Item;
use DVDoug\BoxPacker\Packer;
use DVDoug\BoxPacker\Rotation;

require __DIR__ . '/vendor/autoload.php';

$delayMilliseconds = getenv('COMPARE_DELAY_MS');
if ($delayMilliseconds !== false && $delayMilliseconds !== '') {
    if (!preg_match('/^[0-9]+$/D', $delayMilliseconds)) {
        throw new InvalidArgumentException('COMPARE_DELAY_MS must be an unsigned integer');
    }
    $delay = (int) $delayMilliseconds;
    if ($delay > 60_000) {
        throw new InvalidArgumentException('COMPARE_DELAY_MS exceeds 60000');
    }
    usleep($delay * 1000);
}

final readonly class ComparisonBox implements Box
{
    public function getReference(): string { return 'box'; }
    public function getOuterWidth(): int { return 4; }
    public function getOuterLength(): int { return 1; }
    public function getOuterDepth(): int { return 1; }
    public function getEmptyWeight(): int { return 0; }
    public function getInnerWidth(): int { return 4; }
    public function getInnerLength(): int { return 1; }
    public function getInnerDepth(): int { return 1; }
    public function getMaxWeight(): int { return 2; }
}

final readonly class ComparisonItem implements Item
{
    public function __construct(private string $id) {}
    public function getDescription(): string { return $this->id; }
    public function getWidth(): int { return 2; }
    public function getLength(): int { return 1; }
    public function getDepth(): int { return 1; }
    public function getWeight(): int { return 1; }
    public function getAllowedRotation(): Rotation { return Rotation::BestFit; }
}

$packer = new Packer();
$packer->addBox(new ComparisonBox());
$packer->addItem(new ComparisonItem('a'));
$packer->addItem(new ComparisonItem('b'));
$started = hrtime(true);
$packedBoxes = $packer->pack();
$solveNanoseconds = hrtime(true) - $started;

$containers = [];
foreach ($packedBoxes as $boxIndex => $packedBox) {
    $placements = [];
    foreach ($packedBox->items as $packedItem) {
        $placements[] = [
            'item_id' => $packedItem->item->getDescription(),
            'x' => $packedItem->x,
            'y' => $packedItem->y,
            'z' => $packedItem->z,
            'width' => $packedItem->width,
            'length' => $packedItem->length,
            'depth' => $packedItem->depth,
            'weight' => $packedItem->item->getWeight(),
        ];
    }
    $containers[] = [
        'id' => sprintf('box#%06d', $boxIndex + 1),
        'type_id' => $packedBox->box->getReference(),
        'placements' => $placements,
    ];
}

echo json_encode([
    'adapter_schema' => 'v2',
    'implementation' => 'dvdoug/BoxPacker',
    'implementation_version' => '4.2.0',
    'implementation_revision' => '4fa822e71109095212a499572822c07bdb7228eb',
    'runtime_version' => PHP_VERSION,
    'timing' => [
        'process_startup_included' => false,
        'autoload_and_fixture_setup_included' => false,
        'verification_included' => false,
        'solve_nanoseconds' => $solveNanoseconds,
    ],
    'containers' => $containers,
], JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES) . PHP_EOL;
