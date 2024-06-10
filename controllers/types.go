package controllers

import (
	"context"

	installationv1 "get.porter.sh/porter/gen/proto/go/porterapis/installation/v1alpha1"
	"google.golang.org/grpc"
)

type PorterClient interface {
	ListInstallations(ctx context.Context, in *installationv1.ListInstallationsRequest, opts ...grpc.CallOption) (*installationv1.ListInstallationsResponse, error)
	ListInstallationLatestOutputs(ctx context.Context, in *installationv1.ListInstallationLatestOutputRequest, opts ...grpc.CallOption) (*installationv1.ListInstallationLatestOutputResponse, error)
}

type ClientConn interface {
	Close() error
}
