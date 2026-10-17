INSTALL_DIRECTORY ?= $(HOME)/.local/bin

.PHONY: build install

build:
	go build -o den .

# install removes the old binary before building, since macOS can kill a binary overwritten in place.
install:
	rm -f $(INSTALL_DIRECTORY)/den
	go build -o $(INSTALL_DIRECTORY)/den .
