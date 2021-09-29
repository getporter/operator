package v1

import (
	"encoding/json"

	"github.com/mitchellh/mapstructure"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PorterConfigSpec defines the desired state of PorterConfig
type PorterConfigSpec struct {
	// Debug specifies if Porter should output debug logs.
	Debug *bool `json:"debug,omitempty"`

	// Namespace is the current Porter namespace.
	Namespace *string `json:"namespace,omitempty"`

	// Experimental specifies which experimental features are enabled.
	Experimental []string `json:"experimental,omitempty"`

	// BuildDriver specifies the name of the current build driver.
	// Requires that the build-drivers experimental feature is enabled.
	BuildDriver *string `json:"buildDriver,omitempty"`

	// DefaultStorage is the name of the storage configuration to use.
	DefaultStorage *string `json:"defaultStorage,omitempty"`

	// DefaultSecrets is the name of the secrets configuration to use.
	DefaultSecrets *string `json:"defaultSecrets,omitempty"`

	// DefaultStoragePlugin is the name of the storage plugin to use when DefaultStorage is unspecified.
	DefaultStoragePlugin *string `json:"defaultStoragePlugin"`

	// DefaultSecretsPlugin is the name of the storage plugin to use when DefaultSecrets is unspecified.
	DefaultSecretsPlugin *string `json:"defaultSecretsPlugin"`

	// Storage is a list of named storage configurations.
	Storage []StorageConfig `json:"storage,omitempty"`

	// Secrets is a list of named secrets configurations.
	Secrets []SecretsConfig `json:"secrets,omitempty"`

	// CustomSettings are settings that are not explicitly defined on PorterConfig but are supported by Porter.
	CustomSettings json.RawMessage `json:"customSettings,omitempty"`
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
	Name         string                     `json:"name"`
	PluginSubKey string                     `json:"plugin"`
	Config       map[string]json.RawMessage `json:"config"`
}

// PorterConfigStatus defines the observed state of PorterConfig
type PorterConfigStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// PorterConfig is the Schema for the porterconfigs API
type PorterConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PorterConfigSpec   `json:"spec,omitempty"`
	Status PorterConfigStatus `json:"status,omitempty"`
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

		MergeMap(targetRaw, overrideRaw)
	}

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
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PorterConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PorterConfig{}, &PorterConfigList{})
}
