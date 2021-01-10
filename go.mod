module get.porter.sh/operator

go 1.15

replace github.com/magefile/mage => github.com/carolynvs/mage v1.10.1-0.20201231152132-ef0b6bffd1ac

require (
	github.com/carolynvs/magex v0.3.0
	github.com/go-logr/logr v0.3.0
	github.com/magefile/mage v1.11.0
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/pkg/errors v0.9.1
	k8s.io/api v0.19.2
	k8s.io/apimachinery v0.19.2
	k8s.io/client-go v0.19.2
	sigs.k8s.io/controller-runtime v0.7.0
)
