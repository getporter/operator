package v1

const (
	LabelJobType            = Prefix + "jobType"
	JobTypeAgent            = "porter-agent"
	JobTypeInstaller        = "bundle-installer"
	LabelSecretType         = Prefix + "secretType"
	SecretTypeConfig        = "porter-config"
	SecretTypeWorkdir       = "workdir"
	LabelManaged            = Prefix + "managed"
	LabelResourceKind       = Prefix + "resourceKind"
	LabelResourceName       = Prefix + "resourceName"
	LabelResourceGeneration = Prefix + "resourceGeneration"
	LabelRetry              = Prefix + "retry"
	FinalizerName           = Prefix + "finalizer"
)
