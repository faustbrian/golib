package domain

import (
	_ "arch/infra" // want `architecture/import-boundary: arch/domain must not import arch/infra`
	_ "arch/infra/approved"
	_ "arch/shared"
)
