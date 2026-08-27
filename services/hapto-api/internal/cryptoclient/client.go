// Package cryptoclient is hapto-api's gRPC client for hapto-crypto. It never
// verifies or generates a signature itself, it only forwards requests to the
// crypto service and returns its answer.
package cryptoclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	haptov1 "github.com/chibuike-kt/hapto-api/gen/hapto/v1"
)

type Client struct {
	conn *grpc.ClientConn
	api  haptov1.CryptoServiceClient
}

func Dial(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, api: haptov1.NewCryptoServiceClient(conn)}, nil
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
