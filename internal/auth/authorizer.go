package auth

import (
	"fmt"

	"github.com/casbin/casbin/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Authorizer struct {
	enforcer *casbin.Enforcer
}

func New(model, policy string) (*Authorizer, error) {
	enforcer, err := casbin.NewEnforcer(model, policy)
	if err != nil {
		return nil, err
	}

	return &Authorizer{
		enforcer: enforcer,
	}, nil
}

func (a *Authorizer) Authorize(subject, action string) error {
	isAllowed, err := a.enforcer.Enforce(subject, action)
	if err != nil {
		return err
	}

	if !isAllowed {
		msg := fmt.Sprintf("%q is not permitted to %q", subject, action)
		st := status.New(codes.PermissionDenied, msg)
		return st.Err()
	}

	return nil
}
