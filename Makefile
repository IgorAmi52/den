INSTALL_DIRECTORY ?= $(HOME)/.local/bin

.PHONY: build install

build:
	go build -o den .

install: build
	mkdir -p "$(INSTALL_DIRECTORY)"
	install -S -m 755 den "$(INSTALL_DIRECTORY)/den"
