package eventsourcing_test

import (
	"bufio"
	"os"
	"slices"
	"strings"
	"testing"
)

const compatibilityMatrixPath = "docs/compatibility/eventsauce-3.9.1.md"

func TestEventSauceCompatibilityBaselineIsPinnedAndComplete(t *testing.T) {
	t.Parallel()

	matrix := readContractFile(t, compatibilityMatrixPath)
	for _, required := range []string{
		"# EventSauce 3.9.1 compatibility matrix",
		"`33ea9b97ec3ac56991caad03b791fee418a43e41`",
		"released on 2026-04-25",
	} {
		if !strings.Contains(matrix, required) {
			t.Fatalf("compatibility baseline is missing %q", required)
		}
	}

	wantPages := []string{
		"Introduction",
		"Installation",
		"Event sourcing",
		"Learning material",
		"Architecture",
		"FAQ",
		"Lifecycle",
		"Changelog",
		"Create an aggregate root",
		"Create events and commands",
		"Configure persistence",
		"Bootstrap",
		"Object mapper serialization",
		"Plain serialization",
		"Testing aggregates",
		"Testing preconditions",
		"Handling exceptions",
		"Asserting event payloads",
		"Testing with time",
		"About message storage",
		"Repository table schema",
		"Illuminate repository",
		"Doctrine 3 repository",
		"Doctrine 2 repository",
		"UUID encoding",
		"Setup consumers",
		"Projections and read models",
		"Process managers",
		"Clock",
		"Event dispatcher",
		"Code generation",
		"Code generation from YAML",
		"About the outbox",
		"Outbox setup and usage",
		"Outbox table schema",
		"Illuminate outbox",
		"Doctrine 3 outbox",
		"Doctrine 2 outbox",
		"Build an outbox",
		"Snapshotting",
		"Snapshot setup",
		"Updating snapshots",
		"Anti-corruption layer",
		"Replaying messages",
		"Database structure",
		"Message internals",
		"Message decoration",
		"Upcasting",
		"Custom repository",
		"Custom dispatcher",
		"Aggregate root with aggregates",
		"Upgrade to 0.6",
		"Upgrade to 0.7",
		"Upgrade to 1.0",
	}
	gotPages, statuses := compatibilityInventory(t, matrix)
	if !slices.Equal(gotPages, wantPages) {
		t.Fatalf("documentation-page inventory = %#v, want %#v", gotPages, wantPages)
	}
	for page, status := range statuses {
		switch status {
		case "Partial", "Designed", "Planned":
			t.Fatalf("compatibility page %q retains incomplete status %q", page, status)
		}
	}
	for _, page := range []string{
		"Introduction",
		"Event sourcing",
		"Changelog",
		"Database structure",
		"Outbox setup and usage",
		"Process managers",
		"Projections and read models",
		"Repository table schema",
		"Replaying messages",
	} {
		if statuses[page] != "Implemented" {
			t.Fatalf(
				"%s compatibility status = %q, want Implemented",
				page,
				statuses[page],
			)
		}
	}
	if statuses["Build an outbox"] != "External" {
		t.Fatalf(
			"Build an outbox compatibility status = %q, want External",
			statuses["Build an outbox"],
		)
	}
	adapterStatuses := compatibilityAdapterStatuses(t, matrix)
	for _, adapter := range []string{
		"Synchronous core dispatch",
		"PostgreSQL",
		"gokafka",
		"goqueue",
		"gooutbox",
		"gotelemetry",
	} {
		if adapterStatuses[adapter] != "Implemented" {
			t.Fatalf(
				"%s adapter status = %q, want Implemented",
				adapter,
				adapterStatuses[adapter],
			)
		}
	}
}

func TestChangelogMaintainsReleasePolicy(t *testing.T) {
	t.Parallel()

	changelog := readContractFile(t, "CHANGELOG.md")
	for _, required := range []string{
		"# Changelog\n",
		"[Keep a Changelog](https://keepachangelog.com/en/1.1.0/)",
		"[Semantic Versioning](https://semver.org/spec/v2.0.0.html)",
		"## [Unreleased]\n",
	} {
		if !strings.Contains(changelog, required) {
			t.Fatalf("changelog policy is missing %q", required)
		}
	}
	if !strings.Contains(changelog, "\n### Added\n\n-") &&
		!strings.Contains(changelog, "\n### Changed\n\n-") &&
		!strings.Contains(changelog, "\n### Removed\n\n-") &&
		!strings.Contains(changelog, "\n### Fixed\n\n-") &&
		!strings.Contains(changelog, "\n### Security\n\n-") {
		t.Fatal("Unreleased changelog has no categorized user-visible entry")
	}
	for _, stale := range []string{
		"unfinished Kafka deployment",
		"retaining partial status",
	} {
		if strings.Contains(changelog, stale) {
			t.Fatalf("changelog retains obsolete release status %q", stale)
		}
	}
}

func TestPublicAPIDocumentationUsesCurrentContracts(t *testing.T) {
	t.Parallel()

	documentation := readContractFile(t, "docs/design/public-api.md")
	for _, current := range []string{
		"ReadGlobal(context.Context, ReadGlobalOptions) (MessageIterator, error)",
		"ErrDuplicateMessageID",
		"ErrMalformedEvent",
		"ErrSnapshotIncompatible",
		"ErrUnsupportedCapability",
		"AppendCommitOutcome",
	} {
		if !strings.Contains(documentation, current) {
			t.Fatalf("public API documentation is missing %q", current)
		}
	}
	for _, stale := range []string{
		"ReadAll(",
		"ReadAllOptions",
		"ErrDuplicateMessage ",
		"ErrCommitUnknown ",
		"ErrIncompatibleVersion ",
		"ErrUnsupported ",
	} {
		if strings.Contains(documentation, stale) {
			t.Fatalf("public API documentation retains stale symbol %q", stale)
		}
	}
}

func TestPackageMapMatchesCodeGenerationDecision(t *testing.T) {
	t.Parallel()

	packageMap := readContractFile(t, "docs/design/package-boundaries.md")
	if strings.Contains(
		packageMap,
		"| `codegen` and `cmd/golib-event-sourcing` nested module |",
	) {
		t.Fatal("package map advertises the intentionally excluded generator module")
	}
	if !strings.Contains(
		packageMap,
		"Code generation is intentionally excluded from the first release",
	) {
		t.Fatal("package map does not explain the first-release generator exclusion")
	}
}

func TestQueueGuideMatchesCompatibilityEvidence(t *testing.T) {
	t.Parallel()

	queueGuide := readContractFile(t, "docs/queue.md")
	if strings.Contains(queueGuide, "Backend conformance evidence remains partial") {
		t.Fatal("queue guide still describes implemented durable evidence as partial")
	}
	for _, required := range []string{
		"digest-pinned Valkey Streams 9.1.0",
		"post-handler acknowledgement",
	} {
		if !strings.Contains(queueGuide, required) {
			t.Fatalf("queue guide is missing durable evidence %q", required)
		}
	}
}

func TestReleaseAuditPublishesFindingsAndResidualRisks(t *testing.T) {
	t.Parallel()

	readme := readContractFile(t, "README.md")
	if !strings.Contains(readme, "[Release hardening findings](docs/release-audit.md)") {
		t.Fatal("README does not link the release hardening findings")
	}
	audit := readContractFile(t, "docs/release-audit.md")
	for _, required := range []string{
		"# Release hardening findings",
		"## Findings",
		"Severity",
		"Disposition",
		"## Residual risks",
		"## Evidence boundary",
	} {
		if !strings.Contains(audit, required) {
			t.Fatalf("release audit is missing %q", required)
		}
	}
}

func compatibilityInventory(
	t *testing.T,
	matrix string,
) ([]string, map[string]string) {
	t.Helper()

	pages := make([]string, 0, 54)
	statuses := make(map[string]string, 54)
	scanner := bufio.NewScanner(strings.NewReader(matrix))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "| [") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) != 8 {
			t.Fatalf("malformed compatibility row: %q", line)
		}
		link := strings.TrimSpace(cells[1])
		closing := strings.Index(link, "](")
		if closing < 2 {
			t.Fatalf("malformed compatibility page link: %q", link)
		}
		page := link[1:closing]
		if _, duplicate := statuses[page]; duplicate {
			t.Fatalf("duplicate compatibility page %q", page)
		}
		status := strings.TrimSpace(cells[6])
		switch status {
		case "Implemented", "Partial", "Designed", "Planned", "Excluded", "External":
		default:
			t.Fatalf("compatibility page %q has unknown status %q", page, status)
		}
		pages = append(pages, page)
		statuses[page] = status
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan compatibility matrix: %v", err)
	}

	return pages, statuses
}

func compatibilityAdapterStatuses(
	t *testing.T,
	matrix string,
) map[string]string {
	t.Helper()

	statuses := make(map[string]string, 6)
	inMatrix := false
	scanner := bufio.NewScanner(strings.NewReader(matrix))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "## Adapter capability matrix" {
			inMatrix = true
			continue
		}
		if inMatrix && strings.HasPrefix(line, "## ") {
			break
		}
		if !inMatrix || !strings.HasPrefix(line, "| ") ||
			strings.HasPrefix(line, "| ---") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) != 6 {
			t.Fatalf("malformed adapter row: %q", line)
		}
		name := strings.Trim(strings.TrimSpace(cells[1]), "`")
		if name == "Adapter" {
			continue
		}
		status := strings.TrimSpace(cells[4])
		switch status {
		case "Implemented", "Partial", "External", "Excluded":
		default:
			t.Fatalf("adapter %q has unknown status %q", name, status)
		}
		statuses[name] = status
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan adapter matrix: %v", err)
	}

	return statuses
}

func readContractFile(t *testing.T, path string) string {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(contents)
}
