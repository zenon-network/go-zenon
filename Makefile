.PHONY: all clean znnd version

GO ?= latest

SERVERMAIN = $(shell pwd)/main.go
BUILDDIR = $(shell pwd)/build

znnd:
	go build -o $(BUILDDIR)/znnd $(SERVERMAIN)
	@echo "Build server done."
	@echo "Run \"$(BUILDDIR)/znnd\" to start server."

clean:
	rm -r $(BUILDDIR)/

all: znnd
