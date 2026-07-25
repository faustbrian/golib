#!/bin/sh
set -eu
go test . -run '^ExampleVerifier_Middleware$' -v
