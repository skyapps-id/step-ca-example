.PHONY: certs proto server client client-nocert

# terbitkan server cert (SAN localhost) + client cert (CN = identitas device) lewat API dashboard
certs:
	@../gen-cert.sh certs server grpc-server localhost 127.0.0.1
	@../gen-cert.sh certs client sensor-01

# generate ulang kode Go dari proto (butuh protoc, protoc-gen-go, protoc-gen-go-grpc)
proto:
	@protoc --go_out=. --go_opt=module=grpc-mtls-example \
		--go-grpc_out=. --go-grpc_opt=module=grpc-mtls-example proto/greeter.proto

server:
	@go run ./server

# client dengan sertifikat → diterima
client:
	@go run ./client

# client tanpa sertifikat → ditolak saat handshake
client-nocert:
	@go run ./client -no-cert
