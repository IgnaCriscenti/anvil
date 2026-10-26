BINARY := anvil
DIST   := dist
PKG    := .

export CGO_ENABLED := 0
GOFLAGS := -trimpath
LDFLAGS := -s -w

PLATFORMS := linux-amd64 linux-arm64 windows-amd64 windows-arm64 darwin-amd64 darwin-arm64
TARGETS   := $(PLATFORMS:%=build-%)

.PHONY: all build run clean $(TARGETS)

all: $(TARGETS)

build:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) $(PKG)

run:
	go run $(PKG)

# build-<os>-<arch>, p. ej. build-linux-amd64 → dist/anvil-linux-amd64
$(TARGETS): build-%:
	GOOS=$(word 1,$(subst -, ,$*)) GOARCH=$(word 2,$(subst -, ,$*)) \
		go build $(GOFLAGS) -ldflags "$(LDFLAGS)$(if $(findstring windows,$*), -H=windowsgui)" \
		-o $(DIST)/$(BINARY)-$*$(if $(findstring windows,$*),.exe) $(PKG)

clean:
	rm -rf $(DIST)
