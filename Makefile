DIST_DIR				= ./dist
GIT_TAG					= $(shell git symbolic-ref -q HEAD || git describe --tags --exact-match)
BINARIES 				= aggregator scraper ingester
GO_BUILD 				= go build -a
GO_BASE_ENV 		= GO111MODULE=on
GO_ENV_DEFAULT	= $(GO_BASE_ENV)
GO_ENV_ARM 			= $(GO_BASE_ENV) GOOS=linux GOARCH=arm GOARM=7
GO_ENV_LINUX		= $(GO_BASE_ENV) GOOS=linux GOARCH=amd64
PLATFORM 				?= DEFAULT
GO_ENV					= ${GO_ENV_${PLATFORM}}

.PHONY: clean all test $(BINARIES)

all: $(BINARIES)

$(BINARIES):
	@mkdir -p $(DIST_DIR)
	$(GO_ENV) $(GO_BUILD) -o $(DIST_DIR)/$@ ./cmd/$@ 

test:
	go test -v -timeout 60s -cover ./...

clean:
	rm -rf $(DIST_DIR)