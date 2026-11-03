// gRPC server dengan mTLS: hanya client yang memegang sertifikat dari step-ca
// (rantai sampai root_ca.crt) yang boleh memanggil RPC.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	pb "grpc-mtls-example/greeterpb"
)

type greeter struct {
	pb.UnimplementedGreeterServer
}

func (greeter) SayHello(ctx context.Context, req *pb.HelloRequest) (*pb.HelloReply, error) {
	cn := clientCN(ctx)
	log.Printf("SayHello from CN=%s (name=%q)", cn, req.GetName())
	return &pb.HelloReply{Message: fmt.Sprintf("hello %s, authenticated as CN=%s", req.GetName(), cn)}, nil
}

// clientCN mengambil identitas client dari sertifikat yang sudah diverifikasi saat handshake.
func clientCN(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "unknown"
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 {
		return "unknown"
	}
	return info.State.VerifiedChains[0][0].Subject.CommonName
}

func main() {
	addr := flag.String("addr", ":50051", "listen address")
	dir := flag.String("certs", "certs", "folder berisi server.crt, server.key, root_ca.crt")
	flag.Parse()

	// server.crt adalah bundle leaf + intermediate → dikirim utuh ke client
	cert, err := tls.LoadX509KeyPair(filepath.Join(*dir, "server.crt"), filepath.Join(*dir, "server.key"))
	if err != nil {
		log.Fatalf("load server cert: %v", err)
	}

	// root_ca.crt = trust anchor untuk memverifikasi client cert;
	// intermediate-nya dikirim client sendiri di dalam bundle client.crt
	rootPEM, err := os.ReadFile(filepath.Join(*dir, "root_ca.crt"))
	if err != nil {
		log.Fatalf("read root ca: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		log.Fatal("root_ca.crt: no certificate found")
	}

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert, // inilah yang membuatnya mTLS
		ClientCAs:    roots,
		MinVersion:   tls.VersionTLS13,
	})

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	s := grpc.NewServer(grpc.Creds(creds))
	pb.RegisterGreeterServer(s, greeter{})

	log.Printf("gRPC mTLS server listening on %s", *addr)
	if err := s.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
