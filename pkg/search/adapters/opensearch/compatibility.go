package opensearch

const (
	// OfficialClientVersion is the reviewed opensearch-go release pinned by this module.
	OfficialClientVersion = "4.7.3"
	// OpenSearch2Version is the supported final OpenSearch 2.x release line.
	OpenSearch2Version = "2.19.3"
	// OpenSearch3Version is the supported OpenSearch 3.x release at implementation time.
	OpenSearch3Version = "3.6.0"
)

// SupportedOpenSearchVersions returns a caller-owned exact conformance matrix.
func SupportedOpenSearchVersions() []string {
	return []string{OpenSearch2Version, OpenSearch3Version}
}
