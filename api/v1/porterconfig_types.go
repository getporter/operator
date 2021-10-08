package v1

import (
	"github.com/davecgh/go-spew/spew"
	"github.com/mitchellh/mapstructure"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// PorterConfigSpec defines the desired state of PorterConfig
type PorterConfigSpec struct {
	// Debug specifies if Porter should output debug logs.
	Debug *bool `json:"debug,omitempty" mapstructure:"debug,omitempty"`

	// DebugPlugins specifies if Porter should output debug logs for the plugins.
	DebugPlugins *bool `json:"debugPlugins,omitempty" mapstructure:"debug-plugins,omitempty"`

	// Namespace is the current Porter namespace.
	Namespace *string `json:"namespace,omitempty" mapstructure:"namespace,omitempty"`

	// Experimental specifies which experimental features are enabled.
	Experimental []string `json:"experimental,omitempty" mapstructure:"experimental,omitempty"`

	// BuildDriver specifies the name of the current build driver.
	// Requires that the build-drivers experimental feature is enabled.
	BuildDriver *string `json:"build-driver,omitempty" mapstructure:"build-driver,omitempty"`

	// DefaultStorage is the name of the storage configuration to use.
	DefaultStorage *string `json:"default-storage,omitempty" mapstructure:"default-storage,omitempty"`

	// DefaultSecrets is the name of the secrets configuration to use.
	DefaultSecrets *string `json:"default-secrets,omitempty" mapstructure:"default-secrets,omitempty"`

	// DefaultStoragePlugin is the name of the storage plugin to use when DefaultStorage is unspecified.
	DefaultStoragePlugin *string `json:"default-storage-plugin,omitempty" mapstructure:"default-storage-plugin"`

	// DefaultSecretsPlugin is the name of the storage plugin to use when DefaultSecrets is unspecified.
	DefaultSecretsPlugin *string `json:"default-secrets-plugin" mapstructure:"default-secrets-plugin"`

	// Storage is a list of named storage configurations.
	Storage []StorageConfig `json:"storage,omitempty" mapstructure:"storage,omitempty"`

	// Secrets is a list of named secrets configurations.
	Secrets []SecretsConfig `json:"secrets,omitempty" mapstructure:"secrets,omitempty"`

	//  TODO(carolynvs): Add custom marshaling so that this field can support unknown extra settings and round trip them
	// CustomSettings are settings that are not explicitly defined on PorterConfig but are supported by Porter.
	// CustomSettings json.RawMessage
}

// SecretsConfig is the plugin stanza for secrets.
type SecretsConfig struct {
	PluginConfig `json:",squash"`
}

// StorageConfig is the plugin stanza for storage.
type StorageConfig struct {
	PluginConfig `json:",squash"`
}

// PluginConfig is a standardized config stanza that defines which plugin to
// use and its custom configuration.
type PluginConfig struct {
	Name         string `json:"name"`
	PluginSubKey string `json:"plugin"`
	// +kubebuilder:pruning:PreserveUnknownFields
	Config runtime.RawExtension `json:"config,omitempty"`
}

// PorterConfigStatus defines the observed state of PorterConfig
type PorterConfigStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// PorterConfig is the Schema for the porterconfigs API
type PorterConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" mapstructure:"metadata,omitempty"`

	Spec   PorterConfigSpec   `json:"spec,omitempty" mapstructure:"spec,omitempty"`
	Status PorterConfigStatus `json:"status,omitempty" mapstructure:"status,omitempty"`
}

// MergeConfig from another PorterConfigSpec. The values from the override are applied
// only when they are not empty.
func (c PorterConfigSpec) MergeConfig(overrides ...PorterConfigSpec) (PorterConfigSpec, error) {
	var targetRaw map[string]interface{}
	if err := mapstructure.Decode(c, &targetRaw); err != nil {
		return PorterConfigSpec{}, err
	}

	for _, override := range overrides {
		var overrideRaw map[string]interface{}
		if err := mapstructure.Decode(override, &overrideRaw); err != nil {
			return PorterConfigSpec{}, err
		}

		targetRaw = MergeMap(targetRaw, overrideRaw)
	}

	spew.Dump(targetRaw)
	if err := mapstructure.Decode(targetRaw, &c); err != nil {
		return PorterConfigSpec{}, err
	}
	return c, nil
}

// MergeConfig from another PorterConfigSpec. The values from the override are applied
// only when they are not empty.
func MergeMap(target, override map[string]interface{}) map[string]interface{} {
	for k, v := range override {
		target[k] = v
	}
	return target
}

// +kubebuilder:object:root=true

// PorterConfigList contains a list of PorterConfig
type PorterConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" mapstructure:"metadata,omitempty"`
	Items           []PorterConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PorterConfig{}, &PorterConfigList{})
}
