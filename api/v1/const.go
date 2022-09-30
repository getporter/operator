package v1

const (
	// DefaultPorterAgentRepository is the default image repository of the Porter
	// Agent to use when it is not configured in the operator.
	DefaultPorterAgentRepository = "ghcr.io/getporter/porter-agent"

	// DefaultPorterAgentVersion is the default version of the Porter Agent to
	// use when it is not configured in the operator.
	//
	// As we test out the operator with new versions of Porter, keep this value
	// up-to-date so that the default version is guaranteed to work.
	DefaultPorterAgentVersion = "v1.0.2"

	// LabelJobType is a label applied to jobs created by the operator. It
	// indicates the purpose of the job.
	LabelJobType = Prefix + "jobType"

	// JobTypeAgent is the value of job type label applied to the Porter Agent.
	JobTypeAgent = "porter-agent"

	// JobTypeInstaller is the value of the job type label applied to the job
	// that runs the bundle.
	JobTypeInstaller = "bundle-installer"

	// LabelSecretType is a label applied to secrets created by the operator. It
	// indicates the purpose of the secret.
	LabelSecretType = Prefix + "secretType"

	// SecretTypeConfig is the value of the secret type label applied to the
	// secret that contains files to copy into the porter home directory.
	SecretTypeConfig = "porter-config"

	// SecretTypeWorkdir is the value of the secret type label applied to the
	// secret that contains files to copy into the working directory of the
	// Porter Agent.
	SecretTypeWorkdir = "workdir"

	// LabelManaged is a label applied to resources created by the Porter
	// Operator.
	LabelManaged = Prefix + "managed"

	LablePlugins = Prefix + "plugins"

	// LabelResourceKind is a label applied to resources created by the Porter
	// Operator, representing the kind of owning resource. It is used to help the
	// operator determine if a resource has already been created.
	LabelResourceKind = Prefix + "resourceKind"

	// LabelResourceName is a label applied to the resources created by the
	// Porter Operator, representing the name of the owning resource. It is used
	// to help the operator determine if a resource has
	// already been created.
	LabelResourceName = Prefix + "resourceName"

	// LabelResourceGeneration is a label applied to the resources created by the
	// Porter Operator, representing the generation of the owning resource. It is
	// used to help the operator determine if a resource has
	// already been created.
	LabelResourceGeneration = Prefix + "resourceGeneration"

	// LabelRetry is a label applied to the resources created by the
	// Porter Operator, representing the retry attempt identifier.
	LabelRetry = Prefix + "retry"

	// FinalizerName is the name of the finalizer applied to Porter Operator
	// resources that should be reconciled by the operator before allowing it to
	// be deleted.
	FinalizerName = Prefix + "finalizer"

	// VolumePorterSharedName is the name of the volume shared between the porter
	// agent and the invocation image.
	VolumePorterSharedName = "porter-shared"

	// VolumePorterSharedPath is the mount path of the volume shared between the
	// porter agent and the invocation image.
	VolumePorterSharedPath = "/porter-shared"

	// VolumePorterConfigName is the name of the volume that contains Porter's config
	// file.
	VolumePorterConfigName = "porter-config"

	// VolumePorterConfigPath is the mount path of the volume containing Porter's
	// config file.
	VolumePorterConfigPath = "/porter-config"

	// VolumePorterWorkDirName is the name of the volume that is used as the Porter's
	// working directory.
	VolumePorterWorkDirName = "porter-workdir"

	// VolumePorterWorkDirPath is the mount path of the volume that is used as the
	// Porter's working directory.
	VolumePorterWorkDirPath = "/porter-workdir"

	// VolumeImgPullSecretName is the name of the volume that contains
	// .docker/config.json file.
	VolumeImgPullSecretName = "img-pull-secret"

	// VolumeImagePullSecretPath is the mount path of the volume containing for docker
	// auth for image pull secrets.
	VolumeImgPullSecretPath = "/home/nonroot"
	// VolumePorterSharedName is the name of the volume shared between the porter
	// agent and the invocation image.
	VolumePorterPluginsName = "porter-plugins"

	// VolumePorterConfigPath is the mount path of the volume containing Porter's
	// config file.
	VolumePorterPluginsPath = "/app/.porter/plugins"
)
