package controllers

import (
	"context"

	installationv1 "get.porter.sh/porter/gen/proto/go/porterapis/installation/v1alpha1"
	"google.golang.org/grpc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	PorterNamespace = "porter-operator-system"
	PorterGRPCName  = "porter-grpc-service"
)

type PorterClient interface {
	ListInstallations(ctx context.Context, in *installationv1.ListInstallationsRequest, opts ...grpc.CallOption) (*installationv1.ListInstallationsResponse, error)
	ListInstallationLatestOutputs(ctx context.Context, in *installationv1.ListInstallationLatestOutputRequest, opts ...grpc.CallOption) (*installationv1.ListInstallationLatestOutputResponse, error)
}

type ClientConn interface {
	Close() error
}

var GrpcDeployment = &appsv1.Deployment{
	ObjectMeta: metav1.ObjectMeta{
		Name:      PorterGRPCName,
		Namespace: PorterNamespace,
		Labels: map[string]string{
			"app": "porter-grpc-service",
		},
	},
	Spec: appsv1.DeploymentSpec{
		Replicas: ptr.To(int32(1)),
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": "porter-grpc-service",
			},
		},
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "porter-grpc-service",
						Image: "ghcr.io/bdegeeter/porter/server:v1.0.0-alpha.5-794-g7168418d",
						Ports: []corev1.ContainerPort{
							{
								Name:          "grpc",
								ContainerPort: 3001,
							},
						},
						Args: []string{"server", "run"},
						VolumeMounts: []corev1.VolumeMount{
							{
								MountPath: "/porter-config",
								Name:      "porter-grpc-service-config-volume",
							},
						},
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2000m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("32Mi"),
							},
						},
					},
				},
			},
		},
	},
}

var GrpcService = &corev1.Service{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "",
		Namespace: PorterNamespace,
	},
}

var GrpcConfigMap = &corev1.ConfigMap{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "",
		Namespace: PorterNamespace,
	},
}
