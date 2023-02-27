package v1

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/opencontainers/go-digest"
	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// AnnotationAgentCfgPluginHash is the label used to store plugin hashes from a AgentConfig definition.
	AnnotationAgentCfgPluginsHash = "agent-config-plugins-hash"

	// KindAgentConfig represents AgentConfig kind value.
	KindAgentConfig = "AgentConfig"
)

// DefaultPlugins is the set of default plugins that will be used by the operator.
var DefaultPlugins = map[string]Plugin{
	"kubernetes": {},
}

// AgentConfigSpec defines the configuration for the Porter agent.
//
// SERIALIZATION NOTE:
//
//		The json serialization is for persisting this to Kubernetes.
//	 The mapstructure tags is used internally for AgentConfigSpec.MergeConfig.
type AgentConfigSpec struct {
	// PorterRepository is the repository for the Porter Agent image.
	// Defaults to ghcr.io/getporter/porter-agent
	// +optional
	PorterRepository string `json:"porterRepository,omitempty" mapstructure:"porterRepository,omitempty"`

	// PorterVersion is the tag for the Porter Agent image.
	// Defaults to a well-known version of the agent that has been tested with the operator.
	// Users SHOULD override this to use more recent versions.
	// +optional
	PorterVersion string `json:"porterVersion,omitempty" mapstructure:"porterVersion,omitempty"`

	// ServiceAccount is the service account to run the Porter Agent under.
	// +optional
	ServiceAccount string `json:"serviceAccount,omitempty" mapstructure:"serviceAccount,omitempty"`

	// StorageClassName is the name of the storage class that Porter will request
	// when running the Porter Agent. It is used to determine what the storage class
	// will be for the volume requested
	StorageClassName string `json:"storageClassName,omitempty" mapstructure:"storageClassName,omitempty"`

	// VolumeSize is the size of the persistent volume that Porter will
	// request when running the Porter Agent. It is used to share data
	// between the Porter Agent and the bundle invocation image. It must
	// be large enough to store any files used by the bundle including credentials,
	// parameters and outputs.
	// +optional
	VolumeSize string `json:"volumeSize,omitempty" mapstructure:"volumeSize,omitempty"`

	// PullPolicy specifies when to pull the Porter Agent image. The default
	// is to use PullAlways when the tag is canary or latest, and PullIfNotPresent
	// otherwise.
	// +optional
	PullPolicy v1.PullPolicy `json:"pullPolicy,omitempty" mapstructure:"pullPolicy,omitempty"`

	// InstallationServiceAccount specifies a service account to run the Kubernetes pod/job for the installation image.
	// The default is to run without a service account.
	// This can be useful for a bundle which is targeting the kubernetes cluster that the operator is installed in.
	// +optional
	InstallationServiceAccount string `json:"installationServiceAccount,omitempty" mapstructure:"installationServiceAccount,omitempty"`

	// RetryLimit specifies the maximum number of retries that a failed agent job will run before being marked as failure.
	// The default is set to 6 the same as the `BackoffLimit` on a kubernetes job.
	RetryLimit *int32 `json:"retryLimit,omitempty" mapstructure:"retryLimit,omitempty"`

	// PluginConfigFile specifies plugins required to run Porter bundles.
	// In order to utilize mapstructure omitempty tag with an embedded struct, this field needs to be a pointer
	// +optional
	PluginConfigFile *PluginFileSpec `json:"pluginConfigFile,omitempty" mapstructure:"pluginConfigFile,omitempty"`
}

// MergeConfig from another AgentConfigSpec. The values from the override are applied
// only when they are not empty.
func (c AgentConfigSpec) MergeConfig(overrides ...AgentConfigSpec) (AgentConfigSpec, error) {
	final := c
	var targetRaw map[string]interface{}
	if err := mapstructure.Decode(c, &targetRaw); err != nil {
		return AgentConfigSpec{}, err
	}

	for _, override := range overrides {
		var overrideRaw map[string]interface{}
		if err := mapstructure.Decode(override, &overrideRaw); err != nil {
			return AgentConfigSpec{}, err
		}

		targetRaw = MergeMap(targetRaw, overrideRaw)
	}

	if err := mapstructure.Decode(targetRaw, &final); err != nil {
		return AgentConfigSpec{}, err
	}

	return final, nil
}

// AgentConfigStatus defines the observed state of AgentConfig
type AgentConfigStatus struct {
	PorterResourceStatus `json:",inline"`
	// The current status of whether the AgentConfig is ready to be used for an AgentAction.
	// +kubebuilder:default:=false
	// +kubebuilder:validation:Type=boolean
	Ready bool `json:"ready"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// AgentConfig is the Schema for the agentconfigs API
type AgentConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentConfigSpec   `json:"spec,omitempty"`
	Status AgentConfigStatus `json:"status,omitempty"`
}

func (ac *AgentConfig) GetStatus() PorterResourceStatus {
	return ac.Status.PorterResourceStatus
}

func (ac *AgentConfig) SetStatus(value PorterResourceStatus) {
	ac.Status.PorterResourceStatus = value
}

// MergeConfigs applies override AgentConfig that's ready to be used for an AgentAction in sequential order.
func (ac AgentConfig) MergeConfigs(overrides ...AgentConfig) (AgentConfig, error) {
	specs := []AgentConfigSpec{ac.Spec}
	cfg := ac
	for _, override := range overrides {
		specs = append(specs, override.Spec)
		// only consider the agent config if it exist
		if override.Name != "" {
			cfg = override
		}
	}
	base := AgentConfigSpec{}
	cfgSpec, err := base.MergeConfig(specs...)
	if err != nil {
		return AgentConfig{}, err
	}
	cfg.Spec = cfgSpec

	return cfg, nil

}

// AgentConfigAdapter is a wrapper of AgentConfig schema. It process the input data so that
// the controller can easily work with the input.
type AgentConfigAdapter struct {
	AgentConfig
	Spec AgentConfigSpecAdapter
}

// NewAgentConfigAdapter creates a new instance of the adapter from a AgentConfig.
func NewAgentConfigAdapter(agentCfg AgentConfig) *AgentConfigAdapter {
	return &AgentConfigAdapter{
		AgentConfig: agentCfg,
		Spec:        NewAgentConfigSpecAdapter(agentCfg.Spec),
	}
}

// GetRetryLabelValue returns a value that is safe to use
// as a label value and represents the retry annotation used
// to trigger reconciliation.
func (ac *AgentConfigAdapter) GetRetryLabelValue() string {
	return getRetryLabelValue(ac.Annotations)
}

// SetRetryAnnotation flags the resource to retry its last operation.
func (ac *AgentConfigAdapter) SetRetryAnnotation(retry string) {
	if ac.Annotations == nil {
		ac.Annotations = make(map[string]string, 1)
	}
	ac.Annotations[AnnotationRetry] = retry
}

// GetPluginsPVCName returns a string that's the hash using plugins spec and the AgentConfig's namespace.
func (ac *AgentConfigAdapter) GetPluginsPVCName() string {
	return ac.Spec.Plugins.GetPVCName(ac.Namespace)
}

// GetPluginsPVCNameAnnotation returns a string that's the hash using plugins spec and the AgentConfig's namespace.
func (ac *AgentConfigAdapter) GetPluginsPVCNameAnnotation() map[string]string {
	return ac.Spec.Plugins.GetPVCNameAnnotation(ac.Namespace)
}

// +kubebuilder:object:root=true

// AgentConfigList contains a list of AgentConfig values.
type AgentConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentConfig{}, &AgentConfigList{})
}

type PluginFileSpec struct {
	// SchemaVersion is the version of the plugins configuration state schema.
	SchemaVersion string `json:"schemaVersion" yaml:"schemaVersion"`

	// Plugins is a map of plugin configuration using plugin name as the key.
	Plugins map[string]Plugin `json:"plugins,omitempty" mapstructure:"plugins,omitempty"`
}

// Plugin represents the plugin configuration.
type Plugin struct {
	FeedURL string `json:"feedURL,omitempty" mapstructure:"feedURL,omitempty"`
	URL     string `json:"url,omitempty" mapstructure:"url,omitempty"`
	Mirror  string `json:"mirror,omitempty" mapstructure:"mirror,omitempty"`
	Version string `json:"version,omitempty" mapstructure:"version,omitempty"`
}

// AgentConfigSpecAdapter is a wrapper of AgentConfigSpec with a list representation of plugins configuration.
type AgentConfigSpecAdapter struct {
	original AgentConfigSpec

	Plugins PluginsConfigList
}

// NewAgentConfigSpecAdapter creates a new instance of the AgentConfigSpecAdapter from a AgentConfigSpec.
func NewAgentConfigSpecAdapter(spec AgentConfigSpec) AgentConfigSpecAdapter {
	plugins := make(map[string]Plugin)
	if spec.PluginConfigFile != nil {
		plugins = spec.PluginConfigFile.Plugins
	}
	return AgentConfigSpecAdapter{
		original: spec,
		Plugins:  NewPluginsList(plugins),
	}
}

// GetPluginsPVCName returns a name used for this agent config plugin persistent volume claim.
// Returns an empty string when no plugins are specified, in which case the PVC should not be mounted
func (c AgentConfigSpecAdapter) GetPluginsPVCName(namespace string) string {
	return c.Plugins.GetPVCName(namespace)
}

// GetPorterImage returns the fully qualified image name of the Porter Agent
// image. Defaults the repository and tag when not set.
func (c AgentConfigSpecAdapter) GetPorterImage() string {
	version := c.original.PorterVersion
	if version == "" {
		// We don't use a mutable tag like latest, or canary because it's a bad practice that we don't want to encourage.
		version = DefaultPorterAgentVersion
	}
	repo := c.original.PorterRepository
	if repo == "" {
		repo = DefaultPorterAgentRepository
	}

	if digest, err := digest.Parse(version); err == nil {
		return fmt.Sprintf("%s@%s", repo, digest)
	}

	return fmt.Sprintf("%s:%s", repo, version)
}

// GetPullPolicy returns the PullPolicy that should be used for the Porter Agent
// (not the bundle). Defaults to PullAlways for latest and canary,
// PullIfNotPresent otherwise.
func (c AgentConfigSpecAdapter) GetPullPolicy() v1.PullPolicy {
	if c.original.PullPolicy != "" {
		return c.original.PullPolicy
	}

	if c.original.PorterVersion == "latest" || c.original.PorterVersion == "canary" || c.original.PorterVersion == "dev" {
		return v1.PullAlways
	}
	return v1.PullIfNotPresent
}

// GetStorageClassName returns the name of the storage class to request for the
// volume.
func (c AgentConfigSpecAdapter) GetStorageClassName() string {
	return c.original.StorageClassName
}

// GetVolumeSize returns the size of the shared volume to mount between the
// Porter Agent and the bundle's invocation image. Defaults to 64Mi.
func (c AgentConfigSpecAdapter) GetVolumeSize() resource.Quantity {
	q, err := resource.ParseQuantity(c.original.VolumeSize)
	if err != nil || q.IsZero() {
		return resource.MustParse("64Mi")
	}
	return q
}

// GetPorterRepository returns the config value of Porter repository.
func (c AgentConfigSpecAdapter) GetPorterRepository() string {
	return c.original.PorterRepository
}

// GetPorterVersion returns the config value of Porter version.
func (c AgentConfigSpecAdapter) GetPorterVersion() string {
	return c.original.PorterVersion
}

// GetServiceAccount returns the config value of service account.
func (c AgentConfigSpecAdapter) GetServiceAccount() string {
	return c.original.ServiceAccount
}

// GetInstallationServiceAccount returns the config value of installation service account.
func (c AgentConfigSpecAdapter) GetInstallationServiceAccount() string {
	return c.original.InstallationServiceAccount
}

// SetRetryAnnotation flags the resource to retry its last operation.
func (c *AgentConfigSpecAdapter) GetRetryLimit() *int32 {
	return c.original.RetryLimit
}

func (c AgentConfigSpecAdapter) ToPorterDocument() ([]byte, error) {
	raw := struct {
		SchemaType    string            `yaml:"schemaType"`
		SchemaVersion string            `yaml:"schemaVersion"`
		Plugins       map[string]Plugin `yaml:"plugins"`
	}{
		SchemaType:    "Plugins",
		SchemaVersion: c.original.PluginConfigFile.SchemaVersion,
		Plugins:       c.Plugins.data,
	}

	return yaml.Marshal(raw)
}

// PluginConfigList is the list implementation of the Plugins map.
// The list is sorted based on the plugin names alphabetically.
type PluginsConfigList struct {
	data map[string]Plugin
	keys []string
}

// NewPluginsList creates a new instance of PluginsConfigList.
func NewPluginsList(ps map[string]Plugin) PluginsConfigList {
	keys := make([]string, 0, len(ps))
	for key := range ps {
		keys = append(keys, key)
	}

	sort.SliceStable(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})

	data := make(map[string]Plugin, len(keys))
	for _, k := range keys {
		data[k] = ps[k]
	}
	return PluginsConfigList{
		data: data,
		keys: keys,
	}
}

// IsZero checks whether the list is empty.
func (op PluginsConfigList) IsZero() bool {
	return len(op.keys) == 0
}

// Add adds a new item into the list.
func (op *PluginsConfigList) Add(name string, p Plugin) {

	op.data[name] = p
	op.keys = append(op.keys, name)
	sort.SliceStable(op.keys, func(i, j int) bool {
		return op.keys[i] < op.keys[j]
	})
}

// GetNames returns an array of plugin names in the list sorted alphabetically.
func (op PluginsConfigList) GetNames() []string {
	return op.keys
}

// GetByName returns a plugin based on its name and true if the plugin is found.
// if a plugin is not found in the list, the function returns an empty plugin and false.
func (op PluginsConfigList) GetByName(name string) (Plugin, bool) {
	p, ok := op.data[name]
	return p, ok
}

// GetPVCName returns a hash of the plugin configs.
// if no plugins are defined, it returns an empty string.
func (op PluginsConfigList) GetPVCName(namespace string) string {
	if len(op.data) == 0 {
		return ""
	}

	input := op.label() + namespace

	return "porter-" + hashString(input)
}

// GetLabels returns a hash of all plugin configs that is safe to use
// as a label value and represents the plugin configuration used
// to trigger reconciliation.
// labels are restricted to alphanumeric and .-_ value.
// the maximum characters a label can contain is 63.
// therefore all URLs will be sanitized before using them as part of
// the label.
func (op PluginsConfigList) GetLabels() map[string]string {
	if len(op.data) == 0 {
		return nil
	}

	return map[string]string{
		LabelManaged:     "true",
		LabelPluginsHash: hashString(op.label()),
	}
}

func (op PluginsConfigList) label() string {
	var plugins []string
	var i int
	for _, k := range op.keys {
		p := op.data[k]

		format := "%s"
		if i > 0 {
			format = "_%s"
		}
		plugins = append(plugins, fmt.Sprintf(format, k))

		if p.FeedURL != "" {
			plugins = append(plugins, fmt.Sprintf("_%s", cleanURL(p.FeedURL)))
		}
		if p.URL != "" {
			plugins = append(plugins, fmt.Sprintf("_%s", cleanURL(p.URL)))
		}
		if p.Mirror != "" {
			plugins = append(plugins, fmt.Sprintf("_%s", cleanURL(p.Mirror)))
		}
		if p.Version != "" {
			plugins = append(plugins, fmt.Sprintf("_%s", p.Version))
		}
		i++
	}

	return strings.Join(plugins, "")
}

// GetPVCNameAnnotation returns a string that's the hash using plugins spec and the AgentConfig's namespace.
func (op PluginsConfigList) GetPVCNameAnnotation(namespace string) map[string]string {
	return map[string]string{AnnotationAgentCfgPluginsHash: op.GetPVCName(namespace)}
}

func cleanURL(inputURL string) string {
	var cleanURL string
	u, err := url.Parse(inputURL)
	if err == nil {
		// Remove the scheme (e.g. "http", "https") from the URL
		cleanURL = strings.Replace(u.String(), u.Scheme+"://", "", -1)
	}

	// Replace all non-alphanumeric, non-._- characters with an underscore
	reg, err := regexp.Compile("[^a-zA-Z0-9._-]+")
	if err != nil {
		return ""
	}
	cleanURL = reg.ReplaceAllString(cleanURL, "_")

	return cleanURL
}

func hashString(input string) string {
	hash := md5.Sum([]byte(input))

	return hex.EncodeToString(hash[:])
}
