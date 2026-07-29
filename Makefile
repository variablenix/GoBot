build:
	mkdir -p bin
	go build -o bin/irc-bot ./cmd/irc-bot
run:
	go run ./cmd/irc-bot
test:
	go test ./...
lint:
	golangci-lint run ./...
docker-build:
	docker build -t gobot .
docker-run:
	docker run --rm --env-file .env gobot
