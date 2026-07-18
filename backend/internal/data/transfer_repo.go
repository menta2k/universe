package data

import (
	"context"
	"fmt"
	"io"
)

// TransferLogger records TFTP transfers (implements tftp.TransferLogger).
type TransferLogger struct {
	data *Data
}

func NewTransferLogger(d *Data) *TransferLogger { return &TransferLogger{data: d} }

func (l *TransferLogger) LogTransfer(ctx context.Context, clientIP, filename string, bytes int64, success bool, errMsg string) {
	_, err := l.data.Pool.Exec(ctx,
		`INSERT INTO tftp_transfers (client_ip, filename, bytes_sent, success, error)
		 VALUES (NULLIF($1,'')::inet, $2, $3, $4, $5)`,
		clientIP, filename, bytes, success, errMsg)
	if err != nil {
		// Logging must never break serving; swallow after recording nothing.
		_ = err
	}
}

// ForeignOfferSink records competing DHCP offers (dhcp.ConflictSink).
type ForeignOfferSink struct {
	data *Data
}

func NewForeignOfferSink(d *Data) *ForeignOfferSink { return &ForeignOfferSink{data: d} }

func (s *ForeignOfferSink) RecordForeignOffer(ctx context.Context, serverID, clientMAC, offeredIP string) {
	_, _ = s.data.Pool.Exec(ctx,
		`INSERT INTO dhcp_offers_seen (server_id, client_mac, offered_ip)
		 VALUES ($1::inet, $2::macaddr, NULLIF($3,'')::inet)`,
		serverID, clientMAC, offeredIP)
}

// TFTPFileSource adapts the artifact store to tftp.FileSource for ipxe_bin
// artifacts (embedded iPXE binaries are served by the tftp package directly).
type TFTPFileSource struct {
	store *ArtifactStore
}

func NewTFTPFileSource(store *ArtifactStore) *TFTPFileSource { return &TFTPFileSource{store: store} }

func (s *TFTPFileSource) Open(ctx context.Context, filename string) (io.ReadCloser, int64, error) {
	art, err := s.store.GetByFilename(ctx, filename)
	if err != nil {
		return nil, 0, fmt.Errorf("artifact %q: %w", filename, err)
	}
	rc, err := s.store.Open(ctx, art)
	if err != nil {
		return nil, 0, err
	}
	return rc, art.SizeBytes, nil
}
