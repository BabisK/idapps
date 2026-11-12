// Package config reads the launcher's environment-backed configuration.
package config

import "github.com/spf13/viper"

// Environment returns an isolated configuration bound to the standard platform
// and Docker variables used by the launcher. Empty values retain their usual
// meaning of being unset.
func Environment() *viper.Viper {
	v := viper.New()
	for _, key := range []string{
		"XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME",
		"LOCALAPPDATA", "APPDATA", "DOCKER_CONFIG", "DOCKER_CONTEXT", "DOCKER_HOST",
	} {
		if err := v.BindEnv(key, key); err != nil {
			panic(err) // All keys are static and non-empty.
		}
	}
	return v
}
