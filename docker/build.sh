#!/bin/bash

cd ..
export WORKDIR=/go/src/api-gateway && docker run --rm -ti -v $PWD:$WORKDIR -w $WORKDIR golang:1.7.1-alpine go build -v
mv api-gateway docker/go-api-gateway

cd docker
docker build -t docker-go-api-gateway:0.0.6-dev .

# docker run -ti --rm --name go-api-gateway --link go-input-validation:go-input-validation -e BUSINESS_GO_INPUT_VALIDATION_URL=http://go-input-validation:8080  docker-go-api-gateway:0.0.4-dev

