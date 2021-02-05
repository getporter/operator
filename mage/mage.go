package mage

import (
	"github.com/magefile/mage/mg"
)

// Must stops the build if there is an error
func Must(err error) {
	if err != nil {
		panic(mg.Fatal(1, err))
	}
}
