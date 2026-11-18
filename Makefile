INSTALL_DIRECTORY ?= $(HOME)/.local/bin
# -S makes macOS's install replace the binary safely while den runs; GNU install already does and reads -S as a suffix.
SAFE_INSTALL_FLAG := $(if $(filter Darwin,$(shell uname -s)),-S,)

.PHONY: build install

build:
	go build -o den .

install: build
	mkdir -p "$(INSTALL_DIRECTORY)"
	install $(SAFE_INSTALL_FLAG) -m 755 den "$(INSTALL_DIRECTORY)/den"
