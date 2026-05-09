IMAGE ?= ghcr.io/mfeldheim/klyra
TAG   ?= dev

.PHONY: build test docker dev lint clean

build:
	cd ui && npm ci && npm run build
	cp -r ui/dist internal/server/dist
	go build -ldflags="-s -w" -o klyra .

test:
	go test ./...

lint:
	go vet ./...

docker:
	docker build -t $(IMAGE):$(TAG) .
	docker push $(IMAGE):$(TAG)

dev:
	go run . --kubeconfig=$(HOME)/.kube/config --addr=:8080

clean:
	rm -f klyra
	rm -rf internal/server/dist ui/dist
