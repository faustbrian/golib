package domain

import _ "arch/infra" // want `architecture/import-boundary: arch/domain must not import arch/infra`

import _ "arch/backend/client" // want `architecture/import-boundary: arch/backend/client may only be imported by an approved adapter`

import _ "arch/catalog" // want `architecture/import-boundary: arch/domain must not import arch/catalog across context orders -> catalog`

import (
	_ "arch/infra/approved"
	_ "arch/shared"
)
