package pluginsdk

import "context"

const EntitlementMembershipPro = "membership.pro"

// Entitlements asks the host for the current commercial feature grant.
// Plugins must check at the point of use because a license can expire or be
// revoked after the process was loaded.
type Entitlements interface {
	HasEntitlement(context.Context, string) bool
}
