build:
	go build -i

test:
	go test -cover
	go vet
	golint

cover:
	go test -coverprofile=coverage.out
	go tool cover -html=coverage.out

release:
	env GOOS=linux GOARCH=amd64 ./release.sh
	env GOOS=linux GOARCH=arm ./release.sh
	env GOOS=darwin GOARCH=amd64 ./release.sh
	env GOOS=windows GOARCH=amd64 ./release.sh

fmt:
	gofmt -w *.go

clean:
	go clean
