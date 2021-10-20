package docker

import (
	"encoding/json"
	"errors"
)

func ExtractRepoDigest(inspectOutput string) (string, error) {
	var imgDefRaw []map[string]interface{}
	if err := json.Unmarshal([]byte(inspectOutput), &imgDefRaw); err != nil {
		return "", err
	}

	if len(imgDefRaw) > 0 {
		if repoDigests, ok := imgDefRaw[0]["RepoDigests"]; ok {
			if digests, ok := repoDigests.([]interface{}); ok {
				if len(digests) > 0 {
					return digests[0].(string), nil
				}
			}
		}
	}

	return "", errors.New("No repository digests are associated with the image. Did you push it?")
}
