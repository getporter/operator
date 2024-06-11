package controllers

import (
	"context"

	installationv1 "get.porter.sh/porter/gen/proto/go/porterapis/installation/v1alpha1"
	"google.golang.org/grpc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PorterClient interface {
	ListInstallations(ctx context.Context, in *installationv1.ListInstallationsRequest, opts ...grpc.CallOption) (*installationv1.ListInstallationsResponse, error)
	ListInstallationLatestOutputs(ctx context.Context, in *installationv1.ListInstallationLatestOutputRequest, opts ...grpc.CallOption) (*installationv1.ListInstallationLatestOutputResponse, error)
}

type ClientConn interface {
	Close() error
}

var GrpcDeployment = &appsv1.Deployment{
	ObjectMeta: metav1.ObjectMeta{},
}

var GrpcService = &corev1.Service{
	ObjectMeta: metav1.ObjectMeta{},
}
