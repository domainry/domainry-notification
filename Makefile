.PHONY: fmt-check test integration integration-race vet boundary architecture check

fmt-check:
	@files="$$(find . -name '*.go' -not -path './.git/*' -print | xargs gofmt -l)"; \
	if [ -n "$$files" ]; then \
		printf 'gofmt needed:\n%s\n' "$$files" >&2; \
		exit 1; \
	fi

test:
	go test ./...

integration:
	go test -count=1 ./internal/assembly/module ./internal/assembly/saas ./internal/transport/http/saas

integration-race:
	go test -race -count=1 ./internal/assembly/module ./internal/assembly/saas ./internal/transport/http/saas

vet:
	go vet ./...

boundary:
	@deps="$$(go list -deps ./...)"; \
	if printf '%s\n' "$$deps" | grep -Eq '^github\.com/domainry/domainry-plane(/|$$)|^github\.com/domainry/domainry-connectors(/|$$)'; then \
		printf 'forbidden Plane or concrete Connectors dependency detected\n' >&2; \
		exit 1; \
	fi

architecture:
	@for package in application contract model repository service port migration common util runtime; do \
		if [ -d "$$package" ] && find "$$package" -type f | grep -q .; then \
			printf 'forbidden generic top-level package: %s\n' "$$package" >&2; \
			exit 1; \
		fi; \
	done

check: fmt-check test vet boundary architecture
