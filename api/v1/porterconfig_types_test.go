package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/pointer"
)

func TestPorterConfigSpec_MergeConfig(t *testing.T) {
	t.Run("empty is ignored", func(t *testing.T) {
		nsConfig := PorterConfigSpec{
			Debug: pointer.BoolPtr(true),
		}

		instConfig := PorterConfigSpec{}

		config, err := nsConfig.MergeConfig(instConfig)
		require.NoError(t, err)
		assert.Equal(t, pointer.BoolPtr(true), config.Debug)
	})

	t.Run("override", func(t *testing.T) {
		nsConfig := PorterConfigSpec{
			Debug: pointer.BoolPtr(true),
		}

		instConfig := PorterConfigSpec{
			Debug: pointer.BoolPtr(false),
		}

		config, err := nsConfig.MergeConfig(instConfig)
		require.NoError(t, err)
		assert.Equal(t, pointer.BoolPtr(false), config.Debug)
	})
}
