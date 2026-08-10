package opensearch

const (
	// OfficialClientVersion is the reviewed opensearch-go release pinned by this module.
	OfficialClientVersion = "4.7.3"
	// OpenSearch2Version is the supported final OpenSearch 2.x release line.
	OpenSearch2Version = "2.19.6"
	// OpenSearch3Version is the supported current OpenSearch 3.x release.
	OpenSearch3Version = "3.8.0"
)

// SupportedOpenSearchVersions returns a caller-owned exact conformance matrix.
func SupportedOpenSearchVersions() []string {
	return []string{OpenSearch2Version, OpenSearch3Version}
}
