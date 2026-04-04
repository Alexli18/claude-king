.PHONY: build test test-integration test-all vet fmt clean install install-user uninstall uninstall-user

BINDIR ?= .
SYSTEM_BINDIR = /usr/local/bin
USER_BINDIR = $(HOME)/.local/bin

build:
	go build -o $(BINDIR)/king ./cmd/king
	go build -o $(BINDIR)/king-vassal ./cmd/king-vassal
	go build -o $(BINDIR)/kingctl ./cmd/kingctl

# System-wide install (requires sudo)
install: build
	sudo cp king king-vassal kingctl $(SYSTEM_BINDIR)/
	@echo "Installed to $(SYSTEM_BINDIR)"

# User install — no sudo, patches PATH in shell config if needed
install-user: build
	@mkdir -p $(USER_BINDIR)
	@cp king king-vassal kingctl $(USER_BINDIR)/
	@if ! echo "$$PATH" | tr ':' '\n' | grep -qx "$(USER_BINDIR)"; then \
		for rc in "$(HOME)/.zshrc" "$(HOME)/.bashrc"; do \
			if [ -f "$$rc" ]; then \
				echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$$rc"; \
				echo "Added $(USER_BINDIR) to PATH in $$rc"; \
			fi; \
		done; \
		echo "Restart your shell or run: export PATH=\"$(USER_BINDIR):\$$PATH\""; \
	else \
		echo "$(USER_BINDIR) already in PATH"; \
	fi
	@echo "Installed to $(USER_BINDIR)"

uninstall:
	sudo rm -f $(SYSTEM_BINDIR)/king $(SYSTEM_BINDIR)/king-vassal $(SYSTEM_BINDIR)/kingctl
	@echo "Removed from $(SYSTEM_BINDIR)"

uninstall-user:
	rm -f $(USER_BINDIR)/king $(USER_BINDIR)/king-vassal $(USER_BINDIR)/kingctl
	@echo "Removed from $(USER_BINDIR)"

test:
	go test ./... -race -timeout 60s

test-integration:
	go test ./tests/integration/ -tags integration -v -timeout 120s

test-all: test test-integration

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -f king king-vassal kingctl

.DEFAULT_GOAL := build
