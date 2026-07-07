package pluginrpc

// ProcessCredentials configures the OS user/group used to start a third-party
// plugin process. It is only enforced when the host process has the operating
// system privileges required to set child credentials.
type ProcessCredentials struct {
	UID              uint32
	GID              uint32
	AdditionalGroups []uint32
}
