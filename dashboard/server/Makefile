.PHONY: run build ui dev clean

# jalankan dashboard backend (compile seluruh package folder ini — idiom Go)
run:
	@go run .

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
