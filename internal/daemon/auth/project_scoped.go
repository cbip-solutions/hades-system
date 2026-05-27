// SPDX-License-Identifier: MIT
// Package auth — project_scoped.go.
//
// inv-hades-146: every project-scoped /v1/* endpoint validates the
// caller's effective-project set BEFORE delegating to the substrate +
// emits audit.access_denied{caller_uid, project_id, route, ts} on 403.
//
// Two flavours:
// - ProjectScopedMiddleware — wraps GET handlers (alias from query param).
// Alias is extracted before the body is touched, so no body buffering.
// - CheckProjectScope — called explicitly by POST handlers after they
// have already JSON-decoded the body (avoids body-buffering middleware).
//
// OperatorIDFromContext resolves the operator's stable ID from the peer-cred
// uid + the ProjectResolver stored in context. Replaces the provisional
// knowledgeOperatorFromContext / stateOperatorFromContext stubs shipped by
// H-2 / H-5 (those TODO markers reference this function).
//
// Per-route enforcement is fully covered by project_scoped_test.go + middleware_test.go.
package auth

import (
	"context"
	"errors"
	"net/http"

	"slices"
)

var ErrProjectScope = errors.New("project scope denied")

var ErrProjectAliasNotFound = errors.New("project alias not found")

type ProjectResolver interface {
	OperatorProjects(ctx context.Context, uid uint32) ([]string, error)

	ResolveAlias(ctx context.Context, alias string) (string, error)

	OperatorID(ctx context.Context, uid uint32) (string, error)

	EmitAccessDenied(ctx context.Context, uid uint32, projectID, route string) error
}

type projectResolverKey struct{}

func WithProjectResolver(ctx context.Context, resolver ProjectResolver) context.Context {
	return context.WithValue(ctx, projectResolverKey{}, resolver)
}

func projectResolverFromContext(ctx context.Context) (ProjectResolver, bool) {
	v := ctx.Value(projectResolverKey{})
	r, ok := v.(ProjectResolver)
	return r, ok
}

func ProjectScopedMiddleware(resolver ProjectResolver, extractor func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			alias := extractor(r)
			if alias == "" {
				http.Error(w, `{"error":"project_id required"}`, http.StatusBadRequest)
				return
			}

			pid, err := resolver.ResolveAlias(r.Context(), alias)
			if err != nil || pid == "" {
				http.Error(w, `{"error":"project alias not found"}`, http.StatusNotFound)
				return
			}

			cred := PeerCredFromContext(r.Context())
			if !cred.HasSet {
				http.Error(w, `{"error":"peer-cred required"}`, http.StatusUnauthorized)
				return
			}

			scopes, err := resolver.OperatorProjects(r.Context(), cred.UID)
			if err != nil {
				http.Error(w, `{"error":"resolver error"}`, http.StatusInternalServerError)
				return
			}

			if !slices.Contains(scopes, pid) {
				_ = resolver.EmitAccessDenied(r.Context(), cred.UID, pid, r.URL.Path)
				http.Error(w, `{"error":"project scope denied"}`, http.StatusForbidden)
				return
			}

			ctx := WithProjectResolver(r.Context(), resolver)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func CheckProjectScope(ctx context.Context, alias, route string) error {
	resolver, ok := projectResolverFromContext(ctx)
	if !ok {
		return errors.New("project resolver missing from context")
	}

	pid, err := resolver.ResolveAlias(ctx, alias)
	if err != nil || pid == "" {
		return ErrProjectAliasNotFound
	}

	cred := PeerCredFromContext(ctx)
	if !cred.HasSet {
		return errors.New("peer-cred missing from context")
	}

	scopes, err := resolver.OperatorProjects(ctx, cred.UID)
	if err != nil {
		return err
	}

	if !slices.Contains(scopes, pid) {
		_ = resolver.EmitAccessDenied(ctx, cred.UID, pid, route)
		return ErrProjectScope
	}
	return nil
}

func ResolveCallerProjects(ctx context.Context) ([]string, bool) {
	resolver, ok := projectResolverFromContext(ctx)
	if !ok {
		return nil, false
	}

	cred := PeerCredFromContext(ctx)
	if !cred.HasSet {
		return nil, false
	}

	projects, err := resolver.OperatorProjects(ctx, cred.UID)
	if err != nil {
		return nil, false
	}
	return projects, true
}

func OperatorIDFromContext(ctx context.Context) string {
	resolver, ok := projectResolverFromContext(ctx)
	if !ok {
		return ""
	}

	cred := PeerCredFromContext(ctx)
	if !cred.HasSet {
		return ""
	}

	id, err := resolver.OperatorID(ctx, cred.UID)
	if err != nil {
		return ""
	}
	return id
}
