build:
	go build -i

test:
	go test

release:
	env GOOS=linux GOARCH=amd64 ./release.sh
	env GOOS=linux GOARCH=arm ./release.sh
	env GOOS=darwin GOARCH=amd64 ./release.sh
	env GOOS=windows GOARCH=amd64 ./release.sh

clean:
	go clean
