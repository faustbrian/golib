package aliasuse

import infrastructure "arch/infra" // want `architecture/import-boundary: arch/aliasuse must not import arch/infra across layer application -> infrastructure`

var _ = infrastructure.Marker
