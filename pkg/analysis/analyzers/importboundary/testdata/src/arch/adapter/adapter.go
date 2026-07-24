package adapter

import "arch/backend/client"

// Open delegates backend access through an approved adapter.
func Open() { client.Open[int]() }
