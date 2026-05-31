.PHONY: build

build:
	GOOS=linux GOARCH=arm GOARM=7 go build -o localsend/bin/localsendd main.go
