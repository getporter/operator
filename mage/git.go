package mage

import (
	"io/ioutil"
	"log"
	"os"
	"sync"

	"github.com/carolynvs/magex/shx"
	"github.com/pkg/errors"
)

var gitMetadata GitMetadata
var loadMetadata sync.Once

type GitMetadata struct {
	// Permalink is the version alias, e.g. latest, or canary
	Permalink string

	// Version is the tag or tag+commit hash
	Version string

	// Commit is the hash of the current commit
	Commit string
}

// LoadMetadatda populates the status of the current working copy: current version, tag and permalink
func LoadMetadatda() GitMetadata {
	loadMetadata.Do(func() {
		gitMetadata = GitMetadata{}

		// Get a description of the commit, e.g. v0.30.1 (latest) or v0.30.1-32-gfe72ff73 (canary)
		version, err := shx.OutputS("git", "describe", "--tags")
		if err == nil {
			gitMetadata.Version = version
		} else {
			gitMetadata.Version = "v0"
		}

		// Use latest for tagged commits, otherwise it's a canary build
		err = shx.RunS("git", "describe", "--tags", "--exact-match")
		if err == nil {
			gitMetadata.Permalink = "latest"
		} else {
			gitMetadata.Permalink = "canary"
		}

		commit, err := shx.OutputE("git", "rev-parse", "--short", "HEAD")
		if err == nil {
			gitMetadata.Commit = commit
		}
	})

	log.Println("Permalink:", gitMetadata.Permalink)
	log.Println("Version:", gitMetadata.Version)
	log.Println("Commit:", gitMetadata.Commit)

	if githubEnv, ok := os.LookupEnv("GITHUB_ENV"); ok {
		err := ioutil.WriteFile(githubEnv, []byte("PERMALINK="+gitMetadata.Permalink), 0644)
		Must(errors.Wrapf(err, "couldn't persist PERMALINK to a GitHub Actions environment variable"))
	}

	return gitMetadata
}
