// gRPC client dengan mTLS: memverifikasi server lewat root_ca.crt dan
// membuktikan identitasnya sendiri dengan client.crt dari step-ca.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"log"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "grpc-mtls-example/greeterpb"
)

func main() {
	addr := flag.String("addr", "localhost:50051", "server address (host harus ada di SAN server cert)")
	dir := flag.String("certs", "certs", "folder berisi client.crt, client.key, root_ca.crt")
	name := flag.String("name", "sensor-01", "nama yang dikirim di request")
	noCert := flag.Bool("no-cert", false, "jangan kirim client cert (simulasi device tanpa identitas)")
	flag.Parse()

	rootPEM, err := os.ReadFile(filepath.Join(*dir, "root_ca.crt"))
	if err != nil {
		log.Fatalf("read root ca: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		log.Fatal("root_ca.crt: no certificate found")
	}

	cfg := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}
	if !*noCert {
		cert, err := tls.LoadX509KeyPair(filepath.Join(*dir, "client.crt"), filepath.Join(*dir, "client.key"))
		if err != nil {
			log.Fatalf("load client cert: %v", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(credentials.NewTLS(cfg)))
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reply, err := pb.NewGreeterClient(conn).SayHello(ctx, &pb.HelloRequest{Name: *name})
	if err != nil {
		log.Fatalf("SayHello: %v", err)
	}
	log.Printf("reply: %s", reply.GetMessage())
}
