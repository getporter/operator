package docs

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/carolynvs/magex/mgx"
	"github.com/carolynvs/magex/shx"
)

const (
	// Triggers a build of the v1 website.
	porterV1Webhook = "https://api.netlify.com/build_hooks/60ca5ba254754934bce864b1"

	// LocalPorterRepositoryEnv is the environment variable used to store the path
	// to a local checkout of Porter.
	LocalPorterRepositoryEnv = "PORTER_REPOSITORY"

	// DefaultPorterSourceDir is the directory where the Porter repo
	// is cloned when LocalPorterRepositoryEnv was not specified.
	DefaultPorterSourceDir = "../porter"
)

var must = shx.CommandBuilder{StopOnError: true}

// DeployWebsite triggers a Netlify build for the website.
func DeployWebsite() error {
	// Put up a page on the preview that redirects to the live site
	os.MkdirAll("docs/public", 0755)
	err := copy("hack/website-redirect.html", "docs/public/index.html")
	if err != nil {
		return err
	}

	return TriggerNetlifyDeployment(porterV1Webhook)
}

func copy(src string, dest string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}

	destF, err := os.Create(dest)
	if err != nil {
		return err
	}

	_, err = io.Copy(destF, srcF)
	return err
}

// TriggerNetlifyDeployment builds a netlify site using the specified webhook
func TriggerNetlifyDeployment(webhook string) error {
	emptyMsg := "{}"
	data := strings.NewReader(emptyMsg)
	fmt.Println("POST", webhook)
	fmt.Println(emptyMsg)

	r, err := http.Post(webhook, "application/json", data)
	if err != nil {
		return err
	}

	if r.StatusCode >= 300 {
		defer r.Body.Close()
		msg, _ := io.ReadAll(r.Body)
		return fmt.Errorf("request failed (%d) %s: %s", r.StatusCode, r.Status, msg)
	}

	return nil
}

// DeployWebsitePreview builds the entire website for a preview on Netlify.
func DeployWebsitePreview() error {
	pwd, _ := os.Getwd()
	websiteDir := ensurePorterRepository()
	os.Setenv("PORTER_OPERATOR_REPOSITORY", pwd)

	// Set BASE_URL so that huge understands where the site is hosted
	deployURL := os.Getenv("DEPLOY_PRIME_URL") + "/"

	// Build the website using our local changes
	err := shx.Command("go", "run", "mage.go", "-v", "docs").
		In(websiteDir).Env("BASEURL=" + deployURL).RunV()
	if err != nil {
		return err
	}

	return shx.Copy(filepath.Join(websiteDir, "docs/public"), "docs/public", shx.CopyRecursive)
}

// Preview builds the entire website and previews it in the browser on your local machine.
func Preview() error {
	pwd, _ := os.Getwd()
	websiteDir := ensurePorterRepository()
	os.Setenv("PORTER_OPERATOR_REPOSITORY", pwd)

	// Build the website using our local changes
	return shx.Command("go", "run", "mage.go", "docsPreview").
		In(websiteDir).RunV()
}

// Ensures that we have an operator repository and returns its location
func ensurePorterRepository() string {
	repoPath, err := ensurePorterRepositoryIn(os.Getenv(LocalPorterRepositoryEnv), DefaultPorterSourceDir)
	mgx.Must(err)
	return repoPath
}

// Checks if the repository in localRepo exists and return it
// otherwise clone the repository into defaultRepo, updating with the latest changes if already cloned.
func ensurePorterRepositoryIn(localRepo string, defaultRepo string) (string, error) {
	// Check if we are using a local repo
	if localRepo != "" {
		if _, err := os.Stat(localRepo); err != nil {
			log.Printf("%s %s does not exist, ignoring\n", LocalPorterRepositoryEnv, localRepo)
			os.Unsetenv(LocalPorterRepositoryEnv)
		} else {
			log.Printf("Using porter repository at %s\n", localRepo)
			return localRepo, nil
		}
	}

	// Clone the repo, and ensure it is up-to-date
	cloneDestination, _ := filepath.Abs(defaultRepo)
	_, err := os.Stat(filepath.Join(cloneDestination, ".git"))
	if err != nil && !os.IsNotExist(err) {
		return "", err
	} else if err == nil {
		log.Println("Porter repository already cloned, updating")
		if err = shx.Command("git", "fetch").In(cloneDestination).Run(); err != nil {
			return "", err
		}
		if err = shx.Command("git", "reset", "--hard", "FETCH_HEAD").In(cloneDestination).Run(); err != nil {
			return "", err
		}
		return cloneDestination, nil
	}

	log.Println("Cloning porter repository")
	os.RemoveAll(cloneDestination) // if the path existed but wasn't a git repo, we want to remove it and start fresh
	if err = shx.Run("git", "clone", "-b", "release/v1", "https://github.com/getporter/porter.git", cloneDestination); err != nil {
		return "", err
	}
	return cloneDestination, nil
}
