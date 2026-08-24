.PHONY: fmt-check test vet boundary check

fmt-check:
	@files="$$(find . -name '*.go' -not -path './.git/*' -print | xargs gofmt -l)"; \
	if [ -n "$$files" ]; then \
		printf 'gofmt needed:\n%s\n' "$$files" >&2; \
		exit 1; \
	fi

test:
	go test ./...

vet:
	go vet ./...

boundary:
	@deps="$$(go list -deps ./...)"; \
	if printf '%s\n' "$$deps" | grep -Eq '^github\.com/domainry/domainry-plane(/|$$)|^github\.com/domainry/domainry-connectors(/|$$)'; then \
		printf 'forbidden Plane or concrete Connectors dependency detected\n' >&2; \
		exit 1; \
	fi

check: fmt-check test vet boundary
