#!/bin/sh
set -eu
go test . -run '^ExampleDeliverer_Deliver$' -v
