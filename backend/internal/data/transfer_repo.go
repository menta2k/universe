package data

import (
	"context"
	"fmt"
	"io"

	"universe/backend/internal/biz"
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

// TransferRepo reads unified file-serving history for the admin API (FR-011).
type TransferRepo struct {
	data *Data
}

func NewTransferRepo(d *Data) *TransferRepo { return &TransferRepo{data: d} }

// transferUnion selects TFTP transfers unioned with HTTP file_served events
// into a single shape: (time, client_ip, filename, bytes_sent, success, error,
// protocol). HTTP events carry no client IP column, so it is empty there.
const transferUnion = `
	SELECT time, host(client_ip) AS client_ip, filename, bytes_sent, success, error, 'tftp'::text AS protocol
	FROM tftp_transfers
	UNION ALL
	SELECT time, ''::text AS client_ip, coalesce(detail->>'file', '') AS filename,
	       coalesce((detail->>'bytes')::bigint, 0) AS bytes_sent,
	       (outcome = 'ok') AS success, coalesce(detail->>'error', '') AS error, 'http'::text AS protocol
	FROM provisioning_events WHERE phase = 'file_served'`

// ListTransfers returns a page of transfer records ordered newest first,
// optionally filtered to an exact filename (empty filename means no filter).
func (r *TransferRepo) ListTransfers(ctx context.Context, filename string, page, pageSize int) ([]biz.Transfer, int64, error) {
	var total int64
	if err := r.data.Pool.QueryRow(ctx,
		`SELECT count(*) FROM (`+transferUnion+`) u WHERE ($1 = '' OR filename = $1)`,
		filename).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count transfers: %w", err)
	}
	p, size := normalizePage(page, pageSize)
	rows, err := r.data.Pool.Query(ctx,
		`SELECT time, client_ip, filename, bytes_sent, success, error, protocol
		 FROM (`+transferUnion+`) u
		 WHERE ($1 = '' OR filename = $1)
		 ORDER BY time DESC LIMIT $2 OFFSET $3`,
		filename, size, (p-1)*size)
	if err != nil {
		return nil, 0, fmt.Errorf("list transfers: %w", err)
	}
	defer rows.Close()
	var out []biz.Transfer
	for rows.Next() {
		var t biz.Transfer
		if err := rows.Scan(&t.Time, &t.ClientIP, &t.Filename, &t.BytesSent,
			&t.Success, &t.Error, &t.Protocol); err != nil {
			return nil, 0, fmt.Errorf("scan transfer: %w", err)
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
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
