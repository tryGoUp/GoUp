package plugins

import (
	"github.com/mirkobrombin/go-foundation/v2/core/contracts"
	"github.com/mirkobrombin/goup/internal/plugin"
)

// Compile-time contract assertions: every registered plugin must satisfy the
// plugin.Plugin interface. A method removed or re-signed breaks the build here
// instead of failing at registration time.
var (
	_ = contracts.Assert[plugin.Plugin](&AuthPlugin{})
	_ = contracts.Assert[plugin.Plugin](&CustomHeaderPlugin{})
	_ = contracts.Assert[plugin.Plugin](&DockerBasePlugin{})
	_ = contracts.Assert[plugin.Plugin](&DockerStandardPlugin{})
	_ = contracts.Assert[plugin.Plugin](&NodeJSPlugin{})
	_ = contracts.Assert[plugin.Plugin](&PHPPlugin{})
	_ = contracts.Assert[plugin.Plugin](&PythonPlugin{})
)
