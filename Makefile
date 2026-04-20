.PHONY: all run clean restart kill

CONSOLE_KEY ?= my-secret-key

all: build

build:
	go build -o cli-proxy-api ./cmd/server
	go build -o cli-console ./cmd/console

kill:
	@pkill -9 -f "cli-proxy-api" 2>/dev/null; \
	pkill -9 -f "cli-console" 2>/dev/null; \
	sleep 1; \
	echo "Done"

restart: kill build
	@MANAGEMENT_PASSWORD=$(CONSOLE_KEY) nohup ./cli-proxy-api --no-browser > /tmp/cli-proxy-api.log 2>&1 &
	@nohup ./cli-console --key=$(CONSOLE_KEY) > /tmp/cli-console.log 2>&1 &
	@sleep 2
	@echo "API: http://localhost:8317"
	@echo "Console: http://localhost:8318"
	@echo "Logs: /tmp/cli-proxy-api.log, /tmp/cli-console.log"
	@open http://localhost:8318 2>/dev/null || true

run: restart

clean: kill
	@rm -f cli-proxy-api cli-console
