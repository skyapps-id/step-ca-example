.PHONY: run build ui uibuild free-port clean

PORT ?= 8080

# jalankan dashboard backend (compile seluruh package folder ini — idiom Go)
# port dipaksa: proses lain yang memakai $(PORT) dimatikan dulu
run: free-port
	@LISTEN=:$(PORT) go run .

# matikan proses yang LISTEN di $(PORT) (SIGTERM, lalu SIGKILL kalau masih hidup)
free-port:
	@pids=$$(lsof -tiTCP:$(PORT) -sTCP:LISTEN); \
	if [ -n "$$pids" ]; then \
		echo "port $(PORT) dipakai PID $$pids — mematikan..."; \
		kill $$pids; sleep 1; \
		pids=$$(lsof -tiTCP:$(PORT) -sTCP:LISTEN); \
		[ -z "$$pids" ] || kill -9 $$pids; \
	fi

# build binary saja
build:
	@go build -o dashboard-server .

# jalankan UI (terminal terpisah)
ui:
	@cd ../ui && npm run dev

# build UI untuk production
uibuild:
	@cd ../ui && npm run build

clean:
	@rm -f dashboard-server
