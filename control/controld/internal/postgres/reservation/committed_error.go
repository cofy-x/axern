package reservation

import grpcstatus "google.golang.org/grpc/status"

type committedAdmissionError struct {
	err error
}

func (e committedAdmissionError) Error() string {
	return e.err.Error()
}

func (e committedAdmissionError) Unwrap() error {
	return e.err
}

func (e committedAdmissionError) CommitTransaction() bool {
	return true
}

func (e committedAdmissionError) GRPCStatus() *grpcstatus.Status {
	type grpcStatusError interface {
		GRPCStatus() *grpcstatus.Status
	}
	if st, ok := e.err.(grpcStatusError); ok {
		return st.GRPCStatus()
	}
	return grpcstatus.Convert(e.err)
}
