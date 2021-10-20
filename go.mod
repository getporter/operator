module get.porter.sh/operator

go 1.16

// These replace  statements should be kept in sync with the ones in Porter's go.mod
replace (
	// Use Porter's cnab-go
	github.com/cnabio/cnab-go => github.com/carolynvs/cnab-go v0.20.2-0.20210805155536-9a543e0636f4

	// return-digest
	github.com/cnabio/cnab-to-oci => github.com/carolynvs/cnab-to-oci v0.3.0-beta4.0.20210812163007-0766f78b7ee1

	// See https://github.com/hashicorp/go-plugin/pull/127 and
	// https://github.com/hashicorp/go-plugin/pull/163
	// Also includes a branch we haven't PR'd yet: capture-yamux-logs
	// Tagged from v1.4.0, the improved-configuration branch
	github.com/hashicorp/go-plugin => github.com/getporter/go-plugin v1.4.0-improved-configuration.1

	// go.mod doesn't propogate replacements in the dependency graph so I'm copying this from github.com/moby/buildkit
	github.com/jaguilar/vt100 => github.com/tonistiigi/vt100 v0.0.0-20190402012908-ad4c4a574305

	// Fixes https://github.com/spf13/viper/issues/761
	github.com/spf13/viper => github.com/getporter/viper v1.7.1-porter.2.0.20210514172839-3ea827168363
)

require (
	get.porter.sh/porter v1.0.0-alpha.5
	github.com/carolynvs/magex v0.6.0
	github.com/go-logr/logr v0.3.0
	github.com/magefile/mage v1.11.0
	github.com/mitchellh/mapstructure v1.3.3
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/opencontainers/go-digest v1.0.0
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.7.0
	github.com/tidwall/pretty v1.0.0
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b
	k8s.io/api v0.20.1
	k8s.io/apimachinery v0.20.1
	k8s.io/client-go v0.20.1
	k8s.io/utils v0.0.0-20201110183641-67b214c5f920
	sigs.k8s.io/controller-runtime v0.7.0
)
