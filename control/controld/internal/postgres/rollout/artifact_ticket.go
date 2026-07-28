package pgrollout

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const artifactTicketAudience = "axern.gatewayd.artifact-data"

type artifactTicket struct {
	ArtifactID string `json:"artifact_id"`
	Generation int64  `json:"generation"`
	Digest     string `json:"digest"`
	Size       int64  `json:"size"`
	Expires    int64  `json:"expires"`
	Audience   string `json:"audience"`
}

func NormalizeArtifactTicketKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("artifact ticket signing key is required")
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) >= 32 {
		return decoded, nil
	}
	if len(raw) >= 32 {
		return []byte(raw), nil
	}
	return nil, fmt.Errorf("artifact ticket signing key must contain at least 32 bytes")
}

func (s *Store) ListArtifacts(ctx context.Context, rolloutID, episodeID string) ([]*rolloutv1.Artifact, error) {
	rolloutID = strings.TrimSpace(rolloutID)
	var exists bool
	if err := s.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM rollouts WHERE rollout_id=$1)`, rolloutID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, status.Error(codes.NotFound, "rollout not found")
	}
	query := artifactSelectSQL() + ` WHERE rollout_id=$1`
	args := []any{rolloutID}
	if strings.TrimSpace(episodeID) != "" {
		query += ` AND episode_id=$2`
		args = append(args, strings.TrimSpace(episodeID))
	}
	query += ` ORDER BY episode_id,created_at,artifact_id`
	rows, err := s.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*rolloutv1.Artifact
	for rows.Next() {
		record, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record.artifact)
	}
	return out, rows.Err()
}
func (s *Store) PrepareArtifactDownload(ctx context.Context, id string, ttl time.Duration) (*rolloutv1.Artifact, string, time.Time, bool, error) {
	if len(s.artifactTicketKey) < 32 {
		return nil, "", time.Time{}, false, status.Error(codes.FailedPrecondition, "artifact ticket signing is not configured")
	}
	record, err := scanArtifact(s.db.Pool().QueryRow(ctx, artifactSelectSQL()+` WHERE artifact_id=$1`, strings.TrimSpace(id)))
	if err == pgx.ErrNoRows {
		return nil, "", time.Time{}, false, nil
	}
	if err != nil {
		return nil, "", time.Time{}, false, err
	}
	if record.artifact.GetStatus() != rolloutv1.ArtifactStatus_ARTIFACT_STATUS_PRESENT {
		return nil, "", time.Time{}, true, status.Error(codes.FailedPrecondition, "artifact is not present")
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	expires := s.now().UTC().Add(ttl)
	ticket, err := s.signArtifactTicket(artifactTicket{ArtifactID: record.artifact.GetID(), Generation: record.artifact.GetExecutionGeneration(), Digest: record.artifact.GetDigest(), Size: record.artifact.GetSizeBytes(), Expires: expires.Unix(), Audience: artifactTicketAudience})
	return record.artifact, ticket, expires, true, err
}
func (s *Store) ResolveArtifactDownload(ctx context.Context, ticket string, offset int64, ttl time.Duration) (*rolloutv1.Artifact, string, map[string]string, time.Time, error) {
	claims, err := s.verifyArtifactTicket(ticket, s.now().UTC())
	if err != nil {
		return nil, "", nil, time.Time{}, err
	}
	record, err := scanArtifact(s.db.Pool().QueryRow(ctx, artifactSelectSQL()+` WHERE artifact_id=$1`, claims.ArtifactID))
	if err == pgx.ErrNoRows {
		return nil, "", nil, time.Time{}, status.Error(codes.NotFound, "artifact not found")
	}
	if err != nil {
		return nil, "", nil, time.Time{}, err
	}
	artifact := record.artifact
	if artifact.GetStatus() != rolloutv1.ArtifactStatus_ARTIFACT_STATUS_PRESENT || artifact.GetExecutionGeneration() != claims.Generation || artifact.GetDigest() != claims.Digest || artifact.GetSizeBytes() != claims.Size {
		return nil, "", nil, time.Time{}, status.Error(codes.PermissionDenied, "artifact ticket no longer matches durable artifact state")
	}
	if offset < 0 || offset > artifact.GetSizeBytes() {
		return nil, "", nil, time.Time{}, status.Error(codes.InvalidArgument, "artifact offset is out of range")
	}
	if s.artifacts == nil {
		return nil, "", nil, time.Time{}, status.Error(codes.FailedPrecondition, "artifact store is not configured")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	url, expires, err := s.artifacts.PresignDownload(ctx, record.objectKey, ttl)
	headers := map[string]string{}
	if offset > 0 {
		headers["Range"] = fmt.Sprintf("bytes=%d-", offset)
	}
	return artifact, url, headers, expires, err
}
func (s *Store) signArtifactTicket(claims artifactTicket) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.artifactTicketKey)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (s *Store) verifyArtifactTicket(value string, now time.Time) (artifactTicket, error) {
	var claims artifactTicket
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return claims, status.Error(codes.PermissionDenied, "invalid artifact ticket")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, status.Error(codes.PermissionDenied, "invalid artifact ticket")
	}
	mac := hmac.New(sha256.New, s.artifactTicketKey)
	_, _ = mac.Write([]byte(parts[0]))
	expected := mac.Sum(nil)
	if len(signature) != len(expected) || subtle.ConstantTimeCompare(signature, expected) != 1 {
		return claims, status.Error(codes.PermissionDenied, "invalid artifact ticket")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(payload, &claims) != nil {
		return claims, status.Error(codes.PermissionDenied, "invalid artifact ticket")
	}
	if claims.Audience != artifactTicketAudience {
		return claims, status.Error(codes.PermissionDenied, "invalid artifact ticket audience")
	}
	if claims.Expires <= now.Unix() {
		return claims, status.Error(codes.PermissionDenied, "artifact ticket expired")
	}
	return claims, nil
}
