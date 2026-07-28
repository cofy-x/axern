package appservice

import (
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func serviceAdmissionBlocked(err error) bool {
	switch grpcstatus.Code(err) {
	case codes.FailedPrecondition, codes.ResourceExhausted:
		return true
	default:
		return false
	}
}
