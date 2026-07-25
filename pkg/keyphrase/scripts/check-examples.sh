#!/bin/sh
set -eu

go test -count=1 -run '^Example' ./...
