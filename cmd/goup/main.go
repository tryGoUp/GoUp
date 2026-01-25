package main

import (
	"github.com/mirkobrombin/goup/internal/cli"
	"github.com/mirkobrombin/goup/internal/plugin"
	"github.com/mirkobrombin/goup/plugins"
)

func main() {
	// Load global configuration
	// Global config is now loaded via CLI PersistentPreRun to support flags

	pluginManager := plugin.NewPluginManager()
	plugin.SetDefaultPluginManager(pluginManager)

	// Register your plugins here:
	pluginManager.Register(&plugins.CustomHeaderPlugin{})
	pluginManager.Register(&plugins.PHPPlugin{})
	pluginManager.Register(&plugins.AuthPlugin{})
	pluginManager.Register(&plugins.NodeJSPlugin{})
	pluginManager.Register(&plugins.PythonPlugin{})
	pluginManager.Register(&plugins.DockerBasePlugin{}) // currently here for testig purposes
	pluginManager.Register(&plugins.DockerStandardPlugin{})

	cli.Execute()
}
