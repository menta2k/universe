package service

import (
	"context"
	"errors"
	"net/http"

	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/menta2k/universe/backend/api/netboot/v1"
	"github.com/menta2k/universe/backend/internal/biz"
	"github.com/menta2k/universe/backend/internal/server"
)

// ArtifactService serves the proto RPCs (list/get/delete/transfers) and the
// multipart upload/replace HTTP endpoints (registered via RegisterMultipart).
type ArtifactService struct {
	v1.UnimplementedArtifactServiceServer
	artifacts *biz.ArtifactUsecase
	maxUpload int64
}

// NewArtifactService wires the usecase and the injected upload size cap used to
// bound multipart request bodies before they reach disk.
func NewArtifactService(artifacts *biz.ArtifactUsecase, maxUpload int64) *ArtifactService {
	return &ArtifactService{artifacts: artifacts, maxUpload: maxUpload}
}

// mapArtifactErr maps the in-use guard to 409 and defers to mapErr otherwise.
func mapArtifactErr(err error) error {
	if errors.Is(err, biz.ErrArtifactInUse) {
		return server.ErrConflict("artifact is referenced by a profile release set")
	}
	return mapErr(err)
}

func toArtifactReply(a *biz.Artifact) *v1.BootArtifact {
	return &v1.BootArtifact{
		Id: a.ID, Kind: string(a.Kind), UbuntuRelease: string(a.UbuntuRelease),
		Filename: a.Filename, SizeBytes: a.SizeBytes, Sha256: a.SHA256,
		UploadedBy: a.UploadedBy,
		CreatedAt:  timestamppb.New(a.CreatedAt), UpdatedAt: timestamppb.New(a.UpdatedAt),
	}
}

func (s *ArtifactService) ListArtifacts(ctx context.Context, req *v1.PageRequest) (*v1.ListArtifactsReply, error) {
	page, size := pageParams(req)
	arts, total, err := s.artifacts.List(ctx, page, size)
	if err != nil {
		return nil, mapArtifactErr(err)
	}
	reply := &v1.ListArtifactsReply{Meta: &v1.PageMeta{Total: total, Page: int32(page), PageSize: int32(size)}}
	for _, a := range arts {
		reply.Artifacts = append(reply.Artifacts, toArtifactReply(a))
	}
	return reply, nil
}

func (s *ArtifactService) GetArtifact(ctx context.Context, req *v1.GetArtifactRequest) (*v1.BootArtifact, error) {
	a, err := s.artifacts.Get(ctx, req.GetId())
	if err != nil {
		return nil, mapArtifactErr(err)
	}
	return toArtifactReply(a), nil
}

func (s *ArtifactService) DeleteArtifact(ctx context.Context, req *v1.GetArtifactRequest) (*emptypb.Empty, error) {
	if err := s.artifacts.Delete(ctx, req.GetId()); err != nil {
		return nil, mapArtifactErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *ArtifactService) ListTransfers(ctx context.Context, req *v1.ListTransfersRequest) (*v1.ListTransfersReply, error) {
	page, size := pageParams(req.GetPage())
	transfers, total, err := s.artifacts.ListTransfers(ctx, req.GetFilename(), page, size)
	if err != nil {
		return nil, mapArtifactErr(err)
	}
	reply := &v1.ListTransfersReply{Meta: &v1.PageMeta{Total: total, Page: int32(page), PageSize: int32(size)}}
	for _, t := range transfers {
		reply.Transfers = append(reply.Transfers, &v1.Transfer{
			Time: timestamppb.New(t.Time), ClientIp: t.ClientIP, Filename: t.Filename,
			BytesSent: t.BytesSent, Success: t.Success, Error: t.Error, Protocol: t.Protocol,
		})
	}
	return reply, nil
}

// RegisterMultipart mounts the multipart upload/replace endpoints on the Kratos
// HTTP server. Upload (POST) and Replace (PUT) share one handler: the artifact
// is keyed by filename, so the store upserts by filename regardless of {id}.
// It is intended to be passed to server.NewHTTPServer as a registrar.
func (s *ArtifactService) RegisterMultipart(srv *khttp.Server) {
	r := srv.Route("/")
	r.POST("/api/v1/artifacts", func(ctx khttp.Context) error {
		s.UploadHandler(ctx.Response(), ctx.Request())
		return nil
	})
	r.PUT("/api/v1/artifacts/{id}", func(ctx khttp.Context) error {
		s.UploadHandler(ctx.Response(), ctx.Request())
		return nil
	})
}

// UploadHandler parses a multipart form (fields: kind, ubuntu_release; file
// part: "file"), enforces the injected size cap, calls the usecase, and writes
// the standard {success,data:BootArtifact} envelope. Used for both create and
// replace since artifacts are keyed by filename.
func (s *ArtifactService) UploadHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxUpload+bufferGraceBytes)
	if err := r.ParseMultipartForm(multipartMemoryBytes); err != nil {
		server.ErrorEncoder(w, r, server.ErrValidation("invalid multipart form",
			map[string]string{"file": err.Error()}))
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, header, err := r.FormFile("file")
	if err != nil {
		server.ErrorEncoder(w, r, server.ErrValidation("missing file part",
			map[string]string{"file": "a multipart 'file' part is required"}))
		return
	}
	defer func() { _ = file.Close() }()

	in := biz.UploadInput{
		Kind:          biz.ArtifactKind(r.FormValue("kind")),
		UbuntuRelease: biz.UbuntuRelease(r.FormValue("ubuntu_release")),
		Filename:      header.Filename,
	}
	if op, ok := server.OperatorFromContext(r.Context()); ok {
		in.UploadedBy = op.ID
	}

	art, err := s.artifacts.Upload(r.Context(), in, file)
	if err != nil {
		server.ErrorEncoder(w, r, mapArtifactErr(err))
		return
	}
	_ = server.ResponseEncoder(w, r, toArtifactReply(art))
}

// multipartMemoryBytes bounds the in-memory portion of a parsed multipart form;
// larger file parts spill to temp files. bufferGraceBytes lets the body reader
// admit the file up to the cap plus small field/boundary overhead so the store
// (not the transport) reports an over-size file as a clean validation error.
const (
	multipartMemoryBytes = 16 << 20 // 16 MiB
	bufferGraceBytes     = 1 << 20  // 1 MiB
)
