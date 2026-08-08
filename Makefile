BINARY ?= martie
IMAGE_TAG ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf local)
IMAGE ?= martie:$(IMAGE_TAG)
MARTIE_ENV ?= dev
ENV_FILE ?= .env.$(MARTIE_ENV)
CONFIG_FILE ?= config/$(MARTIE_ENV).toml
CONTAINER ?= martie-$(MARTIE_ENV)
VOLUME ?= martie-$(MARTIE_ENV)-data
DOCKER_RUN_EXTRA ?=
DOCKER_LOG_DRIVER ?= local
DOCKER_NETWORK ?=
GO_BUILD_FLAGS ?= -trimpath -buildvcs=false
LOAD_ENV = set -a; . ./$(ENV_FILE); set +a; \
	CONFIG_FILE=$(CONFIG_FILE); export CONFIG_FILE
DOCKER_RUN_FLAGS = --env-file $(ENV_FILE) \
	-e CONFIG_FILE=/etc/martie/config.toml \
	-e HEALTHCHECK_ADDR=127.0.0.1:9090 \
	--mount type=bind,source=$(abspath $(CONFIG_FILE)),target=/etc/martie/config.toml,readonly \
	--mount type=volume,source=$(VOLUME),target=/data \
	--read-only \
	--tmpfs /tmp:rw,noexec,nosuid,nodev,size=16m \
	--cap-drop ALL \
	--security-opt no-new-privileges
DOCKER_CHECK_CONFIG_FLAGS = --env-file $(ENV_FILE) \
	-e CONFIG_FILE=/etc/martie/config.toml \
	--mount type=bind,source=$(abspath $(CONFIG_FILE)),target=/etc/martie/config.toml,readonly \
	--read-only \
	--tmpfs /data:rw,noexec,nosuid,nodev,size=16m \
	--tmpfs /tmp:rw,noexec,nosuid,nodev,size=16m \
	--cap-drop ALL \
	--security-opt no-new-privileges

ifeq ($(DOCKER_LOG_DRIVER),journald)
DOCKER_LOG_FLAGS = --log-driver journald --log-opt tag=$(CONTAINER)
DOCKER_LOG_COMMAND = journalctl -t $(CONTAINER) -f
else
DOCKER_LOG_FLAGS = --log-driver local --log-opt max-size=10m --log-opt max-file=5
DOCKER_LOG_COMMAND = docker logs -f $(CONTAINER)
endif

ifneq ($(strip $(DOCKER_NETWORK)),)
DOCKER_NETWORK_FLAGS = --network $(DOCKER_NETWORK)
endif

.PHONY: help fmt lint test tidy build run check-config docker-build docker-check-config docker-run docker-deploy docker-logs docker-clean check clean

help:
	@printf '%s\n' \
		'Targets: fmt lint test tidy build run check clean' \
		'Check:   check-config validates the Martie configuration' \
		'Docker:  docker-build docker-check-config docker-run docker-deploy docker-logs docker-clean' \
		'Config:  MARTIE_ENV=dev reads config/dev.toml and .env.dev' \
		'Image:   IMAGE defaults to martie:$(IMAGE_TAG)' \
		'Logs:    DOCKER_LOG_DRIVER=local or journald' \
		'Network: DOCKER_NETWORK=monitoring joins an existing Docker network'

fmt:
	gofmt -w cmd internal

lint:
	go vet ./...

test:
	go test ./...

tidy:
	go mod tidy

build:
	go build $(GO_BUILD_FLAGS) -o $(BINARY) ./cmd/martie

run:
	$(LOAD_ENV); go run $(GO_BUILD_FLAGS) ./cmd/martie run

check-config:
	$(LOAD_ENV); go run $(GO_BUILD_FLAGS) ./cmd/martie check-config

docker-build:
	docker build --pull -t $(IMAGE) .

docker-check-config:
	docker run --rm \
		$(DOCKER_CHECK_CONFIG_FLAGS) \
		$(DOCKER_NETWORK_FLAGS) \
		$(DOCKER_RUN_EXTRA) \
		$(IMAGE) check-config

docker-run:
	docker run -d \
		--name $(CONTAINER) \
		--restart unless-stopped \
		$(DOCKER_RUN_FLAGS) \
		$(DOCKER_LOG_FLAGS) \
		$(DOCKER_NETWORK_FLAGS) \
		$(DOCKER_RUN_EXTRA) \
		$(IMAGE) run

docker-deploy: docker-build docker-check-config
	-docker rm -f $(CONTAINER)
	docker run -d \
		--name $(CONTAINER) \
		--restart unless-stopped \
		$(DOCKER_RUN_FLAGS) \
		$(DOCKER_LOG_FLAGS) \
		$(DOCKER_NETWORK_FLAGS) \
		$(DOCKER_RUN_EXTRA) \
		$(IMAGE) run

docker-logs:
	$(DOCKER_LOG_COMMAND)

docker-clean:
	-docker rm -f martie-dev martie-prod
	-docker volume rm martie-dev-data martie-prod-data

check: fmt lint test

clean:
	rm -f $(BINARY) martie-*
