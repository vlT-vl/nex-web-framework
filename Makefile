# Make targets delegate to small Go scripts.
# build.go is for production artifacts; dev.go is for local HMR.
.PHONY: build release dev obfuscate requirements doctor clean tidy

## build: compile frontend then produce release artifacts for all targets
build:
	go run build.go build

## release: optimised artifacts (-s -w)
release:
	go run build.go release

## dev: Vite HMR + Go backend (daemon disabled, -tags dev)
dev:
	go run dev.go

## obfuscate: release + garble code obfuscation
obfuscate:
	go run build.go obfuscate

## requirements: validate toolchain (go, npm, garble)
requirements:
	go run build.go requirements

## doctor: alias for requirements
doctor:
	go run build.go doctor

## clean: remove release/ and frontend/dist (keeps placeholder)
clean:
	go run build.go clean

## tidy: verify go.mod stays clean
tidy:
	go mod tidy
	@if [ -n "$$(git diff --name-only go.mod go.sum 2>/dev/null)" ]; then \
	  echo "go.mod/go.sum changed after tidy — please commit them"; exit 1; fi
