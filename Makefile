# Installation Directories
SYSCONFDIR ?=$(DESTDIR)/etc/docker
SYSTEMDIR ?=$(DESTDIR)/usr/lib/systemd/system
GOLANG ?= /usr/bin/go
BINARY ?= docker-gfs-plugin
MANINSTALLDIR?= ${DESTDIR}/usr/share/man
BINDIR ?=$(DESTDIR)/usr/libexec/docker

export GO15VENDOREXPERIMENT=1

all: gfs-plugin-build

.PHONY: gfs-plugin-build
gfs-plugin-build: main.go driver.go
	$(GOLANG) build -o $(BINARY) .

.PHONY: install
install:
	if [ ! -f "$(SYSCONFDIR)/docker-gfs-plugin" ]; then					\
	   install -D -m 644 etc/docker/docker-gfs-plugin $(SYSCONFDIR)/docker-gfs-plugin;	\
	fi
	install -D -m 644 systemd/docker-gfs-plugin.service $(SYSTEMDIR)/docker-gfs-plugin.service
	install -D -m 644 systemd/docker-gfs-plugin.socket $(SYSTEMDIR)/docker-gfs-plugin.socket
	install -D -m 755 $(BINARY) $(BINDIR)/$(BINARY)

.PHONY: clean
clean:
	rm -f $(BINARY)


