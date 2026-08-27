// Package cryptoclient is hapto-api's gRPC client for hapto-crypto. It never
// verifies or generates a signature itself, it only forwards requests to the
// crypto service and returns its answer.
//
// The connection is always mutual TLS: hapto-api presents a client
// certificate signed by the shared local CA, and verifies hapto-crypto's
// server certificate against that same CA. There is no plaintext fallback
// — generate certs with scripts/gen-certs.sh.
package cryptoclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	haptov1 "github.com/chibuike-kt/hapto-api/gen/hapto/v1"
)

type Client struct {
	conn *grpc.ClientConn
	api  haptov1.CryptoServiceClient
}

// TLSConfig points at hapto-api's own client certificate/key and the CA
// used to verify hapto-crypto's server certificate.
type TLSConfig struct {
	CertFile string
	KeyFile  string
	CAFile   string
}

func Dial(addr string, tlsCfg TLSConfig) (*Client, error) {
	creds, err := loadClientTLS(tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("load tls config: %w", err)
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, api: haptov1.NewCryptoServiceClient(conn)}, nil
}

func loadClientTLS(cfg TLSConfig) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load client cert/key: %w", err)
	}

	caPEM, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read ca cert: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse ca cert %s: no valid certificates found", cfg.CAFile)
	}

	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS12,
	}), nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) ValidatePublicKey(ctx context.Context, publicKey []byte, algorithm haptov1.SignatureAlgorithm) (valid bool, reason string, err error) {
	resp, err := c.api.ValidatePublicKey(ctx, &haptov1.ValidatePublicKeyRequest{
		PublicKey: publicKey,
		Algorithm: algorithm,
	})
	if err != nil {
		return false, "", err
	}
	return resp.GetValid(), resp.GetReason(), nil
}

func (c *Client) VerifySignature(ctx context.Context, publicKey, message, signature []byte, algorithm haptov1.SignatureAlgorithm) (valid bool, reason string, err error) {
	resp, err := c.api.VerifySignature(ctx, &haptov1.VerifySignatureRequest{
		PublicKey: publicKey,
		Algorithm: algorithm,
		Message:   message,
		Signature: signature,
	})
	if err != nil {
		return false, "", err
	}
	return resp.GetValid(), resp.GetReason(), nil
}

func (c *Client) GenerateNonce(ctx context.Context, sizeBytes uint32) ([]byte, error) {
	resp, err := c.api.GenerateNonce(ctx, &haptov1.GenerateNonceRequest{SizeBytes: sizeBytes})
	if err != nil {
		return nil, err
	}
	return resp.GetNonce(), nil
}
