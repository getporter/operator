module get.porter.sh/operator

go 1.15

require (
	get.porter.sh/porter v1.0.0-alpha.3
	github.com/carolynvs/magex v0.6.0
	github.com/davecgh/go-spew v1.1.1
	github.com/go-logr/logr v0.3.0
	github.com/magefile/mage v1.11.0
	github.com/mitchellh/mapstructure v1.3.3
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/opencontainers/go-digest v1.0.0
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.7.0
	github.com/tidwall/pretty v1.0.0
	k8s.io/api v0.20.1
	k8s.io/apimachinery v0.20.1
	k8s.io/client-go v0.20.1
	k8s.io/utils v0.0.0-20201110183641-67b214c5f920
	sigs.k8s.io/controller-runtime v0.7.0
)
