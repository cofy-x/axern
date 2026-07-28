package publicv1

import (
	"context"
	"testing"
	"time"

	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestNamespaceCreateValidatesNamespace(t *testing.T) {
	server := New(Dependencies{
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
		Namespaces: &fakeNamespaces{},
	})
	_, err := server.CreateNamespace(context.Background(), &namespacev1.CreateNamespaceRequest{})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument err=%v", grpcstatus.Code(err), err)
	}
}

func TestNamespaceCreateStoresNamespace(t *testing.T) {
	namespaces := &fakeNamespaces{}
	server := New(Dependencies{
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
		Namespaces: namespaces,
	})
	resp, err := server.CreateNamespace(context.Background(), &namespacev1.CreateNamespaceRequest{Namespace: "team-a"})
	if err != nil {
		t.Fatalf("CreateNamespace() error = %v", err)
	}
	if resp.GetNamespace().GetNamespace() != "team-a" {
		t.Fatalf("namespace = %q, want team-a", resp.GetNamespace().GetNamespace())
	}
	if namespaces.created != "team-a" {
		t.Fatalf("created = %q, want team-a", namespaces.created)
	}
}

func TestNamespaceGetValidatesNamespace(t *testing.T) {
	server := New(Dependencies{
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
		Namespaces: &fakeNamespaces{},
	})
	_, err := server.GetNamespace(context.Background(), &namespacev1.GetNamespaceRequest{})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument err=%v", grpcstatus.Code(err), err)
	}
}

func TestNamespaceDeleteValidatesNamespace(t *testing.T) {
	server := New(Dependencies{
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
		Namespaces: &fakeNamespaces{},
	})
	_, err := server.DeleteNamespace(context.Background(), &namespacev1.DeleteNamespaceRequest{})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument err=%v", grpcstatus.Code(err), err)
	}
}

func TestNamespaceDeleteStoresNamespace(t *testing.T) {
	namespaces := &fakeNamespaces{}
	server := New(Dependencies{
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
		Namespaces: namespaces,
	})
	resp, err := server.DeleteNamespace(context.Background(), &namespacev1.DeleteNamespaceRequest{Namespace: "team-a"})
	if err != nil {
		t.Fatalf("DeleteNamespace() error = %v", err)
	}
	if resp.GetNamespace().GetNamespace() != "team-a" {
		t.Fatalf("namespace = %q, want team-a", resp.GetNamespace().GetNamespace())
	}
	if namespaces.deleted != "team-a" {
		t.Fatalf("deleted = %q, want team-a", namespaces.deleted)
	}
}

type fakeNamespaces struct {
	created string
	deleted string
}

func (f *fakeNamespaces) CreateNamespace(_ context.Context, namespace string, _ time.Time) (*namespacev1.Namespace, error) {
	f.created = namespace
	return &namespacev1.Namespace{Namespace: namespace, Version: 1}, nil
}

func (f *fakeNamespaces) GetNamespace(_ context.Context, namespace string) (*namespacev1.Namespace, error) {
	return &namespacev1.Namespace{Namespace: namespace, Version: 1}, nil
}

func (f *fakeNamespaces) ListNamespaces(context.Context) ([]*namespacev1.Namespace, error) {
	return []*namespacev1.Namespace{{Namespace: "team-a", Version: 1}}, nil
}

func (f *fakeNamespaces) DeleteNamespace(_ context.Context, namespace string, _ time.Time) (*namespacev1.Namespace, error) {
	f.deleted = namespace
	return &namespacev1.Namespace{Namespace: namespace, Version: 1}, nil
}

var _ Namespaces = (*fakeNamespaces)(nil)
