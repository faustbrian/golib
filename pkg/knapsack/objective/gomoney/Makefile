GO ?= go

.PHONY: docs

docs:
	test -s README.md
	test -s CHANGELOG.md
	test -s LICENSE
	test -s api/baseline.txt
	$(GO) test ./... -run '^Example' -count=1
