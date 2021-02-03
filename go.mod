module get.porter.sh/operator

go 1.15

require (
	get.porter.sh/porter v0.32.0
	github.com/carolynvs/magex v0.3.1-0.20210121165806-e2c237fbee9e
	github.com/go-logr/logr v0.3.0
	github.com/magefile/mage v1.11.0
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.6.1
	github.com/tidwall/pretty v1.0.0
	k8s.io/api v0.19.2
	k8s.io/apimachinery v0.19.2
	k8s.io/client-go v0.19.2
	k8s.io/utils v0.0.0-20200912215256-4140de9c8800
	sigs.k8s.io/controller-runtime v0.7.0
)
