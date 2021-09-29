module get.porter.sh/operator

go 1.15

require (
	github.com/carolynvs/magex v0.5.0
	github.com/go-logr/logr v0.3.0
	github.com/magefile/mage v1.11.0
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/opencontainers/go-digest v1.0.0-rc1
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.6.1
	github.com/tidwall/pretty v1.0.0
	golang.org/x/sys v0.0.0-20200905004654-be1d3432aa8f // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	k8s.io/api v0.19.2
	k8s.io/apimachinery v0.19.2
	k8s.io/client-go v0.19.2
	k8s.io/utils v0.0.0-20200912215256-4140de9c8800
	sigs.k8s.io/controller-runtime v0.7.0
)
