package application

import (
	_ "arch/application/internal"
	_ "arch/catalog" // want `architecture/import-boundary: arch/application must not import arch/catalog across context orders -> catalog`
	_ "arch/contextonly"
	_ "arch/infra" // want `architecture/import-boundary: arch/application must not import arch/infra across layer application -> infrastructure`
	_ "arch/layeronly"
	_ "arch/shared"
	_ "net/http"
)
