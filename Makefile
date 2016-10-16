NAME = rchicoli/docker-go-api-gateway
VERSION = 0.0.7-dev
WORKDIR = /go/src/go-api-gateway
BINARY = go-api-gateway

.PHONY: all build tag release

all: build

build:
	docker run --rm -ti -v $(PWD):$(WORKDIR) -w $(WORKDIR) golang:1.7.1-alpine go build -v
	mv $(BINARY) docker/
	docker build --rm -t $(NAME):$(VERSION) docker/

tag:
	docker tag $(NAME):$(VERSION) $(NAME):latest

release: tag
	docker push $(NAME):$(VERSION)
	docker push $(NAME):latest
