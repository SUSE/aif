package publish

import "context"

// AllowAllAuthorizer is a no-op Authorizer that approves every request.
type AllowAllAuthorizer struct{}

func (AllowAllAuthorizer) Allowed(_ context.Context, _, _, _ string) (bool, error) {
	return true, nil
}
