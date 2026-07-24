//go:build !windows

package application

import _ "arch/infra" // want `architecture/import-boundary: arch/application must not import arch/infra across layer application -> infrastructure`
