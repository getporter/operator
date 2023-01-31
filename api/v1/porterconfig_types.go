package v1

import (
	"encoding/json"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// PorterConfigSpec defines the desired state of PorterConfig
//
// SERIALIZATION NOTE:
//
//	Use json to persist this resource to Kubernetes.
//	Use yaml to convert to Porter's representation of the resource.
//	The mapstructure tags are used internally for PorterConfigSpec.MergeConfig.
type PorterConfigSpec struct {
	// Threshold for printing messages to the console
	// Allowed values are: debug, info, warn, error
	Verbosity *string `json:"verbosity,omitempty" yaml:"verbosity,omitempty" mapstructure:"verbosity,omitempty"`

	// Namespace is the default Porter namespace.
	Namespace *string `json:"namespace,omitempty" yaml:"namespace,omitempty" mapstructure:"namespace,omitempty"`

	// Experimental specifies which experimental features are enabled.
	Experimental []string `json:"experimental,omitempty" yaml:"experimental,omitempty" mapstructure:"experimental,omitempty"`

	// BuildDriver specifies the name of the current build driver.
	// Requires that the build-drivers experimental feature is enabled.
	BuildDriver *string `json:"build-driver,omitempty" yaml:"build-driver,omitempty" mapstructure:"build-driver,omitempty"`

	// DefaultStorage is the name of the storage configuration to use.
	DefaultStorage *string `json:"default-storage,omitempty" yaml:"default-storage,omitempty" mapstructure:"default-storage,omitempty"`

	// DefaultSecrets is the name of the secrets configuration to use.
	DefaultSecrets *string `json:"default-secrets,omitempty" yaml:"default-secrets,omitempty" mapstructure:"default-secrets,omitempty"`

	// DefaultStoragePlugin is the name of the storage plugin to use when DefaultStorage is unspecified.
	DefaultStoragePlugin *string `json:"default-storage-plugin,omitempty" yaml:"default-storage-plugin,omitempty" mapstructure:"default-storage-plugin,omitempty"`

	// DefaultSecretsPlugin is the name of the storage plugin to use when DefaultSecrets is unspecified.
	DefaultSecretsPlugin *string `json:"default-secrets-plugin,omitempty" yaml:"default-secrets-plugin,omitempty" mapstructure:"default-secrets-plugin,omitempty"`

	// Storage is a list of named storage configurations.
	Storage []StorageConfig `json:"storage,omitempty" yaml:"storage,omitempty" mapstructure:"storage,omitempty"`

	// Secrets is a list of named secrets configurations.
	Secrets []SecretsConfig `json:"secrets,omitempty" yaml:"secrets,omitempty" mapstructure:"secrets,omitempty"`
}

// ToPorterDocument converts from the Kubernetes representation of the Installation into Porter's resource format.
func (c PorterConfigSpec) ToPorterDocument() ([]byte, error) {
	b, err := yaml.Marshal(c)
	return b, errors.Wrap(err, "error converting the PorterConfig spec into its Porter resource representation")
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

// SecretsConfig is the plugin stanza for secrets.
type SecretsConfig struct {
	PluginConfig `json:",inline" yaml:",inline" mapstructure:",squash"`
}

// StorageConfig is the plugin stanza for storage.
type StorageConfig struct {
	PluginConfig `json:",inline" yaml:",inline" mapstructure:",squash"`
}

// PluginConfig is a standardized config stanza that defines which plugin to
// use and its custom configuration.
type PluginConfig struct {
	Name         string `json:"name" yaml:"name" mapstructure:"name"`
	PluginSubKey string `json:"plugin" yaml:"plugin" mapstructure:"plugin"`

	// +kubebuilder:pruning:PreserveUnknownFields
	Config runtime.RawExtension `json:"config,omitempty" yaml:"config,omitempty" mapstructure:"config,omitempty"`
}

var _ yaml.Marshaler = PluginConfig{}

// MarshalYAML handles writing the plugin config with its runtime.RawExtension
// which only has special marshal logic for json by default.
func (in PluginConfig) MarshalYAML() (interface{}, error) {
	raw := map[string]interface{}{
		"name":   in.Name,
		"plugin": in.PluginSubKey,
	}
	// Don't add the config for unmarshal unless something is defined
	if len(in.Config.Raw) != 0 {
		var rawCfg = map[string]interface{}{}
		raw["config"] = rawCfg
		if err := json.Unmarshal(in.Config.Raw, &rawCfg); err != nil {
			return nil, errors.Wrap(err, "could not marshal the plugin config to json")
		}
	}

	return raw, nil
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// PorterConfig is the Schema for the porterconfigs API
type PorterConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PorterConfigSpec `json:"spec,omitempty"`
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
